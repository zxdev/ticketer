package ticket

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Ticket
type Ticket struct {
	Path     string        // default is /tmp
	ttl      time.Duration // default is 12 hours
	sequence int32         // default is off; -1
}

// NewTicket is the *Ticket configurator; provides assurance Path exists
func NewTicket(path string) *Ticket {
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrExist) {
		os.MkdirAll(path, 0755)
	}
	return &Ticket{Path: path, sequence: -1}
}

// getPath assures a valid composite ticketed uuid path is returned
func (m *Ticket) getPath(ticket string) string {
	return filepath.Join(m.Path, filepath.Base(ticket))
}

// Queue turns queue sequencer on
func (m *Ticket) Queue() *Ticket { atomic.CompareAndSwapInt32(&m.sequence, -1, 0); return m }

// Generate new ticket uuid; concurrency safe
//
// random   :0  e4e45937-79c9-c3b4-07e4-7c13d989f9235e15
// sequence :1+ 00000001-5d9b-95d2-de8d-9c7cb21451fac9c1
//
// [4]byte header, uint32 identifier
// [2]byte high 32bit unix time
// [2]byte low 32bit unix time
// [2]byte random uint16
// [8]byte random uint64
func (m *Ticket) Generate() string {

	var tkt [18]byte
	if atomic.LoadInt32(&m.sequence) > -1 {
		binary.BigEndian.PutUint64(tkt[:], uint64(time.Now().Unix()))
		binary.BigEndian.PutUint32(tkt[:4], uint32(atomic.AddInt32(&m.sequence, 1)))
		rand.Read(tkt[8:])
		atomic.CompareAndSwapInt32(&m.sequence, 100000000, 1) // 100MM; when to reset
	} else {
		rand.Read(tkt[:])
	}

	return fmt.Sprintf("%x-%x-%x-%x-%x", tkt[0:4], tkt[4:6], tkt[6:8], tkt[8:10], tkt[10:])
}

// Writer creates the ticketed file and returns an io.WriteCloser
func (m *Ticket) Writer(ticket *string) (io.WriteCloser, bool) {

	writer, err := os.Create(m.getPath(*ticket))
	return writer, err == nil

}

// Reader opens the ticketed file and returns an io.ReadCloser
func (m *Ticket) Reader(ticket *string) (io.ReadCloser, bool) {

	reader, err := os.Open(m.getPath(*ticket))
	return reader, err == nil

}

// Save writes data as a ticketed file from io.Reader
func (m *Ticket) Save(ticket *string, reader io.Reader) (string, bool) {

	if ticket == nil {
		tkt := m.Generate()
		ticket = &tkt
	}

	qf, ok := m.Writer(ticket)
	if ok {
		io.Copy(qf, reader)
		qf.Close()
	}

	return *ticket, ok
}

// Load retrieves the ticketed file and copies the data to the io.Writer
func (m *Ticket) Load(ticket *string, writer io.Writer) bool {

	qf, ok := m.Reader(ticket)
	if ok {
		io.Copy(writer, qf)
		qf.Close()
	}

	return ok
}

// Remove a ticketed file from m.Path
func (m *Ticket) Remove(ticket *string) bool { return os.Remove(m.getPath(*ticket)) == nil }

// Next returns the next ticket for processing from m.Path reading in
// directory order, not necessarily fifo; or random selection mixing
func (m *Ticket) Next(random bool) *string {

	var path string
	f, _ := os.Open(m.Path)
	de, err := f.ReadDir(1000)
	f.Close()

	if err != nil || len(de) == 0 {
		return nil
	}

	if !random { // head from directory order
		path = filepath.Join(m.Path, de[0].Name())

	} else {
		var b [8]byte
		rand.Read(b[:]) // generate random uint64, use modulus math for random selection
		path = filepath.Join(m.Path, de[binary.LittleEndian.Uint64(b[:])%uint64(len(de))].Name())

	}

	return &path

}

// Start the ttl ticket expiration manager and immediately call m.Expire(nil) which
// sets default ttl before entering the ticker loop; aborts when in queue mode
func (m *Ticket) Start(ctx context.Context) {

	// abort when in queue mode; -1
	if atomic.LoadInt32(&m.sequence) < 0 {
		return
	}

	// defaults; when not set by NewTicket
	if len(m.Path) == 0 {
		m.Path = "/tmp"
	}
	if _, err := os.Stat(m.Path); !errors.Is(err, fs.ErrExist) {
		os.MkdirAll(m.Path, 0755)
	}
	m.Expire(nil)

	// check hourly for expirations
	ticker := time.NewTicker(time.Hour)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return

		case <-ticker.C:
			m.Expire(nil)
		}
	}

}

// Expire aged tickets in ticket.Path now; nil sets default 12hr
// when not already set or age sets ttl to timeframe specified
func (m *Ticket) Expire(age *time.Duration) *Ticket {

	if age == nil && m.ttl == 0 {
		m.ttl = time.Hour * 12
	}

	if age != nil && *age > 0 {
		m.ttl = *age
	}

	if m.ttl < time.Hour {
		m.ttl = time.Hour
	}

	info, err := os.ReadDir(m.Path)
	if err != nil {
		return nil
	}

	now := time.Now().Truncate(time.Second)
	for i := range info {
		if info[i].Type().IsRegular() {
			if fin, err := info[i].Info(); err == nil &&
				fin.ModTime().Add(m.ttl).Before(now) {
				os.Remove(filepath.Join(m.Path, info[i].Name()))
			}
		}
	}

	return m
}
