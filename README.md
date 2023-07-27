# Smokescreen [![Test](https://github.com/stripe/smokescreen/workflows/Test/badge.svg?branch=master&event=push)](https://github.com/stripe/smokescreen/actions?query=workflow%3ATest+branch%3Amaster) [![Coverage Status](https://coveralls.io/repos/github/stripe/smokescreen/badge.svg?branch=master)](https://coveralls.io/github/stripe/smokescreen?branch=master)

Smokescreen is a HTTP CONNECT proxy. It proxies most traffic from Stripe to the
external world (e.g., webhooks).

Smokescreen restricts which URLs it connects to: it resolves each domain name
that is requested and ensures that it is a publicly routable IP and not a
Stripe-internal IP. This prevents a class of attacks where, for instance, our
own webhooks infrastructure is used to scan Stripe's internal network.

Smokescreen also allows us to centralize egress from Stripe, allowing us to give
financial partners stable egress IP addresses and abstracting away the details
of which Stripe service is making the request.

In typical usage, clients contact Smokescreen over mTLS. Upon receiving a
connection, Smokescreen authenticates the client's certificate against a
configurable set of CAs and CRLs, extracts the client's identity, and checks
the client's requested CONNECT destination against a configurable per-client
ACL.

By default, Smokescreen will identify clients by the "common name" in the TLS
certificate they present, if any. The client identification function can also
be easily replaced; more on this in the usage section.

## Dependencies

Smokescreen uses [go modules][mod] to manage dependencies. The
linked page contains documentation, but some useful commands are reproduced
below:

- **Adding a dependency**: `go build` `go test` `go mod tidy` will automatically fetch the latest version of any new dependencies. Running `go mod vendor` will vendor the dependency.
- **Updating a dependency**: `go get dep@v1.1.1` or `go get dep@commit-hash` will bring in specific versions of a dependency. The updated dependency should be vendored using `go mod vendor`.

Smokescreen uses a [custom fork](https://github.com/stripe/goproxy) of goproxy to allow us to support context passing and setting granular timeouts on proxy connections.

Generally, Smokescreen will only support the two most recent Go versions. See
[the test configuration](.github/workflows/test.yml) for details.

[mod]: https://github.com/golang/go/wiki/Modules

## Usage

### CLI

Here are the options you can give Smokescreen:

```
   --help                                      Show this help text.
   --config-file FILE                          Load configuration from FILE.  Command line options override values in the file.
   --listen-ip IP                              Listen on interface with address IP.
                                                 This argument is ignored when running under Einhorn. (default: any)
   --listen-port PORT                          Listen on port PORT.
                                                 This argument is ignored when running under Einhorn. (default: 4750)
   --timeout DURATION                          Time out after DURATION when connecting. (default: 10s)
   --proxy-protocol                            Enable PROXY protocol support.
   --deny-range RANGE                          Add RANGE(in CIDR notation) to list of blocked IP ranges.  Repeatable.
   --allow-range RANGE                         Add RANGE (in CIDR notation) to list of allowed IP ranges.  Repeatable.
   --deny-address value                        Add IP[:PORT] to list of blocked IPs.  Repeatable.
   --allow-address value                       Add IP[:PORT] to list of allowed IPs.  Repeatable.
   --egress-acl-file FILE                      Validate egress traffic against FILE
   --expose-prometheus-metrics                 Exposes metrics via a Prometheus scrapable endpoint.
   --prometheus-endpoint ENDPOINT              Specify endpoint to host Prometheus metrics on. (default: "/metrics")
                                                 Requires `--expose-prometheus-metrics` to be set.
   --prometheus-port PORT                      Specify port to host Prometheus metrics on. (default "9810")
                                                 Requires `--expose-prometheus-metrics` to be set.
   --resolver-address ADDRESS                  Make DNS requests to ADDRESS (IP:port).  Repeatable.
   --statsd-address ADDRESS                    Send metrics to statsd at ADDRESS (IP:port). (default: "127.0.0.1:8200")
   --tls-server-bundle-file FILE               Authenticate to clients using key and certs from FILE
   --tls-client-ca-file FILE                   Validate client certificates using Certificate Authority from FILE
   --tls-crl-file FILE                         Verify validity of client certificates against Certificate Revocation List from FILE
   --additional-error-message-on-deny MESSAGE  Display MESSAGE in the HTTP response if proxying request is denied
   --disable-acl-policy-action POLICY ACTION   Disable usage of a POLICY ACTION such as "open" in the egress ACL
   --stats-socket-dir DIR                      Enable connection tracking. Will expose one UDS in DIR going by the name of "track-{pid}.sock".
                                                 This should be an absolute path with all symlinks, if any, resolved.
   --stats-socket-file-mode FILE_MODE          Set the filemode to FILE_MODE on the statistics socket (default: "700")
   --version, -v                               print the version
```

### Client Identification

In order to override how Smokescreen identifies its clients, you must:

- Create a new go project
- Import Smokescreen
- Create a Smokescreen configuration using cmd.NewConfiguration
- Replace `smokescreen.Config.RoleFromRequest` with your own `func(request *http.Request) (string, error)`
- Call smokescreen.StartWithConfig
- Build your new project and use the resulting executable through its CLI

Here is a fictional example that would split a client certificate's `OrganizationalUnit` on commas and use the first particle as the service name.

```go
package main

import (...)

func main() {
	// Here is an opportunity to pass your logger
	conf, err := cmd.NewConfiguration(nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	if conf == nil {
		os.Exit(1)
	}

	conf.RoleFromRequest = func(request *http.Request) (string, error) {
		fail := func(err error) (string, error) { return "", err }

		subject := request.TLS.PeerCertificates[0].Subject
		if len(subject.OrganizationalUnit) == 0 {
			fail(fmt.Errorf("warn: Provided cert has no 'OrganizationalUnit'. Can't extract service role."))
		}
		return strings.SplitN(subject.OrganizationalUnit[0], ".", 2)[0], nil
	}

	smokescreen.StartWithConfig(conf, nil)
}
```

### ACLs

An ACL can be described in a YAML formatted file. The ACL, at its top-level, contains a list of services as well as a default behavior.

Three policies are supported:

| Policy  | Behavior                                                                                                       |
| ------- | -------------------------------------------------------------------------------------------------------------- |
| Open    | Allows all traffic for this service                                                                            |
| Report  | Allows all traffic for this service and warns if client accesses a remote host which is not in the list        |
| Enforce | Only allows traffic to remote hosts provided in the list. Will warn and deny if remote host is not in the list |

A host can be specified with or without a globbing prefix. The host (without the globbing prefix) must be in Punycode to prevent ambiguity.

| host                | valid   |
| ------------------- | ------- |
| `example.com`       | yes     |
| `*.example.com`     | yes     |
| `api.*.example.com` | no      |
| `*example.com`      | no      |
| `ex*ample.com`      | no      |
| `éxämple.com`       | no      |
| `example.*`         | hell no |

[Here](https://github.com/stripe/smokescreen/blob/master/pkg/smokescreen/acl/v1/testdata/sample_config.yaml) is a sample ACL.

#### Global Allow/Deny Lists

Optionally, you may specify a global allow list and a global deny list in your ACL config.

These lists override the policy, but do not override the `allowed_domains` list for each role.

For example, specifying `example.com` in your global_allow_list will allow traffic for that domain on that role, even if that role is set to `enforce` and does not specify `example.com` in its allowed domains.

Similarly, specifying `malicious.com` in your global_deny_list will deny traffic for that domain on a role, even if that role is set to `report` or `open`.
However, if the host specifies `malicious.com` in its `allowed_domains`, traffic to `malicious.com` will be allowed on that role, regardless of policy.

> :warning: **The global_deny_list will only block specific *domains*, not entire *destinations*.** For example, if `malicious.com` is in the global_deny_list but the IP address that it resolves to is not, roles with an `open` policy will still be able to access the destination by using its IP address directly. For this reason, **we recommend using allowlists instead of denylists, whenever it is possible to do so.**

If a domain matches both the `global_allow_list` and the `global_deny_list`, the `global_deny_list` behavior takes priority.

[Here](https://github.com/stripe/smokescreen/blob/master/pkg/smokescreen/acl/v1/testdata/sample_config_with_global.yaml) is a sample ACL specifying these options.

# Development and Testing

## Running locally

To run Smokescreen locally, you can provide a minimal configuration file and use `curl` as a client. For example:

```yaml
# config.yaml
---
allow_missing_role: true  # skip mTLS client validation
statsd_address: 127.0.0.1:8200
```

If you want to see metrics Smokescreen emits, listen on a local port:

```shellsession
$ nc -uklv 127.0.0.1 8200
```

Build and run Smokescreen:

```shellsession
$ go run . --config-file config.yaml
{"level":"info","msg":"starting","time":"2022-11-30T15:19:08-08:00"}
```

Make a request using `curl`:

```shellsession
$ curl --proxytunnel -x localhost:4750 https://stripe.com/
```

## Testing

```shellsession
$ go test ./...
```

# Contributors

- Aditya Mukerjee
- Andreas Fuchs
- Andrew Dunham
- Andrew Metcalf
- Aniket Joshi
- Ben Ransford
- Carl Jackson
- Craig Shannon
- Evan Broder
- Marc-André Tremblay
- Ryan Koppenhaver
