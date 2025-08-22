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
	c.srv.l.Debugf("Write buffer created for client %s: size %d.\n", c.value(), WriteBufSize)
	wch.wg.Add(1)
	go wch.start()
	return wch
}

func (wch *writech) Close() {
	wch.once.Do(func() {
		wch.mu.Lock()
		c := wch.c
		wch.closed = true
		close(wch.ch)
		wch.mu.Unlock()

		wch.wg.Wait()

		wch.mu.Lock()
		wch.ch = nil
		wch.mu.Unlock()
		c.srv.l.Debugf("Write buffer for client %s closed.\n", c.value())
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
			wch.c.srv.l.Debugf("Panic caught for client %s: %s\n", wch.c.value(), r)
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
	c.srv.l.Debugf("Write channel for client %s opened.\n", c.value())
	defer c.Close()
	defer wch.Close()
	defer wch.wg.Done()
	for buf := range wch.ch {
		c.srv.l.Interceptf("Sent data to client %s\n%s\n", c.value(), buf)
		// Because data is sent sequentially, set a write deadline.
		deadlineErr := c.conn.SetWriteDeadline(time.Now().Add(WriteDeadlineDuration))
		if deadlineErr != nil {
			c.srv.l.Errorf("SetWriteDeadline failed for client %s: %v\n", c.value(), deadlineErr)
		}
		startTime := time.Now()
		_, err := c.conn.Write(buf)
		if err != nil {
			// if writing fails, log and close the writer
			if !c.isClosed() {
				c.srv.l.Errorf("Write error from client %s: %v\n", c.value(), err)
			}
			return
		}
		c.storeWriteDuration(startTime)
	}
}
