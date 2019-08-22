package conntrack

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestInstrumentedConnByteCounting sends a fixed size message between two
// instrumented connections and ensures the bytes in and out are correct.
func TestInstrumentedConnByteCounting(t *testing.T) {
	assert := assert.New(t)

	addr := "localhost:8888"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	tr := NewTestTracker()
	sent := []byte("X-Smokescreen-Test")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}

		icWriter := tr.NewInstrumentedConn(conn, "test", "localhost")

		n, err := icWriter.Write(sent)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()

		assert.Equal(len(sent), n)
		assert.Equal(uint64(len(sent)), *icWriter.BytesOut)
	}()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	icReader := tr.NewInstrumentedConn(conn, "testBytesInOut", "localhost")

	go func() {
		defer wg.Done()
		read := make([]byte, len(sent))
		for {
			_, err := icReader.Read(read)
			if err != nil {
				if err != io.EOF {
					t.Fatal(err)
				}
				return
			}
			assert.Equal(sent, read)
		}
	}()

	wg.Wait()
	assert.Equal(uint64(len(sent)), *icReader.BytesIn)
}

func TestInstrumentedConnIdle(t *testing.T) {
	assert := assert.New(t)

	tr := NewTestTracker()
	ic := tr.NewInstrumentedConn(&net.UnixConn{}, "testIdle", "localhost")

	ic.Write([]byte("egress"))
	assert.False(ic.Idle())

	time.Sleep(time.Second * 1)
	assert.True(ic.Idle())
}

func NewTestTracker() *Tracker {
	sd := atomic.Value{}
	sd.Store(false)

	return NewTracker(time.Millisecond*500, nil, logrus.New(), sd)
}