package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	settleFor      = 8 * time.Millisecond
	rescanEvery    = 30 * time.Second
	frameBytes     = bitCount / 8
	maxPacketBytes = 1 << 20
)

type packet struct {
	Type    string `json:"type"`
	Session string `json:"session"`

	Seq         float64 `json:"seq"`
	At          float64 `json:"at"`
	Addon       string  `json:"addon"`
	Text        string  `json:"text"`
	Cwd         string  `json:"cwd"`
	Model       string  `json:"model"`
	Effort      string  `json:"effort"`
	Thread      string  `json:"thread"`
	Title       string  `json:"title"`
	Approval    string  `json:"approval"`
	ContextText string  `json:"contextText"`
	Plain       bool    `json:"plain"`

	File string `json:"-"`
}

type packetKey struct {
	session string
	seq     int64
}

type outbox struct {
	dir        string
	files      map[string]*addonList
	dirs       map[string]bool
	seen       map[packetKey]time.Time
	watchDir   func(string) error
	unwatchDir func(string) error
}

type addonList struct {
	mod     time.Time
	size    int64
	changed time.Time
	pending bool
	based   bool
	frames  frameDecoder
}

func newOutbox(dir string) *outbox {
	return &outbox{dir: dir, files: map[string]*addonList{}, dirs: map[string]bool{}, seen: map[packetKey]time.Time{}}
}

func watchOutbox(ctx context.Context, dir string, out chan<- packet) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	o := newOutbox(dir)
	o.watchDir = w.Add
	o.unwatchDir = w.Remove
	return o.watch(ctx, w.Events, w.Errors, out)
}

func (o *outbox) watch(ctx context.Context, events <-chan fsnotify.Event, errs <-chan error, out chan<- packet) error {
	if err := o.scan(time.Now()); err != nil {
		return err
	}
	tick := time.NewTicker(rescanEvery)
	defer tick.Stop()
	settle := time.NewTimer(settleFor)
	defer settle.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if !ok {
				return errors.New("filesystem notifications stopped")
			}
			// An overflow means the system dropped notifications because its queue was full. A rescan reads every
			// AddOns.txt again.
			if !errors.Is(err, fsnotify.ErrEventOverflow) {
				return err
			}
			if err := o.scan(time.Now()); err != nil {
				return err
			}
		case event, ok := <-events:
			if !ok {
				return errors.New("filesystem notifications stopped")
			}
			if !event.Has(fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename) {
				continue
			}
			if strings.EqualFold(filepath.Base(event.Name), "AddOns.txt") {
				if f := o.files[event.Name]; f != nil {
					f.pending = true
				} else if err := o.scan(time.Now()); err != nil {
					return err
				}
			} else {
				changedDir := o.dirs[event.Name]
				if changedDir && event.Has(fsnotify.Remove|fsnotify.Rename) {
					o.unwatchDir(event.Name)
					delete(o.dirs, event.Name)
				}
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					changedDir = err == nil && info.IsDir()
				}
				if !changedDir {
					continue
				}
				if err := o.scan(time.Now()); err != nil {
					return err
				}
			}
		case now := <-tick.C:
			if err := o.scan(now); err != nil {
				return err
			}
		case <-settle.C:
		}
		now := time.Now()
		for _, p := range o.poll(now) {
			select {
			case out <- p:
			case <-ctx.Done():
				return nil
			}
		}
		for _, f := range o.files {
			if f.pending {
				settle.Reset(settleFor)
				break
			}
		}
	}
}

func (o *outbox) poll(now time.Time) []packet {
	var out []packet
	for path, f := range o.files {
		if f.based && !f.pending {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				f.pending = false
			}
			continue
		}
		if !info.ModTime().Equal(f.mod) || info.Size() != f.size {
			f.mod, f.size, f.changed, f.pending = info.ModTime(), info.Size(), now, true
			continue
		}
		if f.pending && now.Sub(f.changed) >= settleFor {
			f.pending = false
			if p, ok := o.read(path, f, now); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

func (o *outbox) scan(now time.Time) error {
	found := map[string]bool{}
	dirs := map[string]bool{}
	watch := func(path string) error {
		dirs[path] = true
		if o.watchDir != nil && !o.dirs[path] {
			if err := o.watchDir(path); err != nil {
				return err
			}
			o.dirs[path] = true
		}
		return nil
	}
	if err := watch(filepath.Dir(o.dir)); err != nil {
		return err
	}
	err := filepath.WalkDir(o.dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		case d.IsDir() && strings.EqualFold(d.Name(), "SavedVariables"):
			return filepath.SkipDir
		case d.IsDir():
			return watch(path)
		case d.Type().IsRegular() && strings.EqualFold(d.Name(), "AddOns.txt"):
			found[path] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	for path := range o.dirs {
		if !dirs[path] {
			if o.unwatchDir != nil {
				o.unwatchDir(path)
			}
			delete(o.dirs, path)
		}
	}
	for path := range found {
		if o.files[path] == nil {
			o.files[path] = &addonList{}
		}
		// Recheck known files too, because a filesystem notification can be lost.
		o.files[path].pending = true
	}
	for path := range o.files {
		if !found[path] {
			// A save can remove the file for a short time. Keep its partial packet until the character folder is
			// gone.
			if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
				delete(o.files, path)
			}
		}
	}
	for key, at := range o.seen {
		if now.Sub(at) > time.Hour {
			delete(o.seen, key)
		}
	}
	return nil
}

func (o *outbox) read(path string, f *addonList, now time.Time) (packet, bool) {
	file, err := openShared(path)
	if err != nil {
		return packet{}, false
	}
	defer file.Close()
	text, err := io.ReadAll(io.LimitReader(file, maxPacketBytes+1))
	if err != nil || len(text) > maxPacketBytes {
		return packet{}, false
	}
	frame, ok := parseFrame(text)
	if !ok {
		return packet{}, false
	}
	if !f.based {
		f.based = true
		f.frames.start(frame)
		return packet{}, false
	}
	data := f.frames.add(frame)
	if data == nil {
		return packet{}, false
	}
	name, _ := filepath.Rel(o.dir, path)
	p, err := decodePacket(data)
	if err != nil {
		logf("Dropped a packet from %s: %v", name, err)
		return packet{}, false
	}

	key := packetKey{p.Session, int64(p.Seq)}
	if _, dup := o.seen[key]; dup {
		return packet{}, false
	}
	o.seen[key] = now
	p.File = name
	return p, true
}

func parseFrame(text []byte) ([frameBytes]byte, bool) {
	var frame [frameBytes]byte
	var seen [bitCount]bool
	count := 0
	for line := range strings.SplitSeq(string(text), "\n") {
		name, state, ok := strings.Cut(strings.TrimSpace(line), ":")
		digits, isBit := strings.CutPrefix(name, "thott-bot-chat-bit-")
		n, err := strconv.Atoi(digits)
		if !ok || !isBit || len(digits) != 3 || err != nil || n < 1 || n > bitCount {
			continue
		}
		switch strings.TrimSpace(state) {
		case "enabled":
			frame[(n-1)/8] |= 0x80 >> ((n - 1) % 8)
		case "disabled":
		default:
			continue
		}
		if !seen[n-1] {
			seen[n-1] = true
			count++
		}
	}
	return frame, count == bitCount
}

type frameDecoder struct {
	last    [frameBytes]byte
	started bool
	buf     []byte
}

func (d *frameDecoder) start(frame [frameBytes]byte) {
	d.last, d.started = frame, true
}

func (d *frameDecoder) add(frame [frameBytes]byte) []byte {

	if d.started && frame == d.last {
		return nil
	}
	if d.started && frame[0]&0x7f != (d.last[0]+1)&0x7f {
		d.buf = nil
	}
	d.last, d.started = frame, true
	switch {
	case frame[0]&0x80 != 0:
		d.buf = append([]byte(nil), frame[1:]...)
	case d.buf != nil:
		d.buf = append(d.buf, frame[1:]...)
	default:
		return nil
	}
	if len(d.buf) < 2 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(d.buf))
	if len(d.buf) < 2+n {
		return nil
	}
	data := d.buf[2 : 2+n]
	d.buf = nil
	return data
}

func decodePacket(data []byte) (packet, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return packet{}, err
	}
	defer r.Close()
	body, err := io.ReadAll(io.LimitReader(r, maxPacketBytes+1))
	if err != nil {
		return packet{}, err
	}
	if len(body) > maxPacketBytes {
		return packet{}, errors.New("packet too large")
	}
	var p packet
	if err := json.Unmarshal(body, &p); err != nil {
		return packet{}, err
	}
	if p.Type == "" || p.Session == "" || p.Seq < 1 || p.Seq >= math.MaxInt64 || p.Seq != math.Trunc(p.Seq) || p.At < 0 || p.At >= math.MaxInt64 || p.At != math.Trunc(p.At) {
		return packet{}, errors.New("packet has an invalid type, session, seq or timestamp")
	}
	return p, nil
}
