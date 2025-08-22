package main

import (
	"net"
	"sync"
	"time"
)

// writech provides a write channel that will read in a goroutine, and write to an underlying net.Conn interface.
type writech struct {
	c      *Client
	ch     chan []byte
	mu     sync.RWMutex
	wg     sync.WaitGroup
	closed bool
	once   sync.Once
}

func newWritech(c *Client) *writech {
	wch := &writech{
		c:  c,
		ch: make(chan []byte, WriteBufSize),
	}
	wch.wg.Add(1)
	go wch.start()
	return wch
}

func (wch *writech) Close() {
	wch.once.Do(func() {
		wch.mu.Lock()
		wch.closed = true
		close(wch.ch)
		wch.mu.Unlock()

		wch.wg.Wait()

		wch.mu.Lock()
		wch.ch = nil
		wch.mu.Unlock()
	})
}

func (wch *writech) Write(p []byte) (err error) {
	if wch.isClosed() {
		// use the standard net error
		return net.ErrClosed
	}

	// Just in case this snuck through, defer in case of panic.
	defer func() {
		if r := recover(); r != nil {
			err = net.ErrClosed
		}
	}()

	wch.ch <- p
	return nil
}

func (wch *writech) isClosed() bool {
	wch.mu.RLock()
	defer wch.mu.RUnlock()
	return wch.closed
}

func (wch *writech) start() {
	c := wch.c
	defer c.Close()
	defer wch.Close()
	defer wch.wg.Done()
	for buf := range wch.ch {
		// Because data is sent sequentially, set a write deadline.
		_ = c.conn.SetWriteDeadline(time.Now().Add(WriteDeadlineDuration))

		startTime := time.Now()
		_, err := c.conn.Write(buf)
		if err != nil {
			// if writing fails, log and close the writer
			if !c.isClosed() {
				if c.id != 0 {
					c.srv.Printf("Write error from client %d: %v\n", c.id, err)
				} else {
					c.srv.Printf("Write error from client %s: %v\n", c.conn.RemoteAddr(), err)
				}
			}
			return
		}
		c.storeWriteDuration(startTime)
	}
}
