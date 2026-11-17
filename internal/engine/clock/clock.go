package clock

import (
	"sync/atomic"
	"time"

	// The browser has no /usr/share/zoneinfo, so carry the IANA database in
	// the binary: it is what makes TZ=Asia/Tehran work at all. Importing it
	// here rather than in cmd/wasm keeps tests on the same zone data.
	_ "time/tzdata"
)

type Clock struct {
	offset atomic.Int64                  // virtual now − real now, in nanoseconds
	local  atomic.Pointer[time.Location] // the captured zone used when TZ is unset
}

// New captures the host timezone when a machine or session starts.
func New() *Clock {
	c := &Clock{}
	// Force Go's lazy local-zone initialization before the host environment changes.
	_ = time.Local.String()
	c.local.Store(time.Local)
	return c
}

// Now is the time the shell believes it is.
func (c *Clock) Now() time.Time { return time.Now().Add(c.Offset()).In(c.localZone()) }

// Set moves the clock to t. It keeps ticking from there, like `date -s` does.
func (c *Clock) Set(t time.Time) { c.SetOffset(time.Until(t)) }

func (c *Clock) Offset() time.Duration {
	if c == nil {
		return 0
	}
	return time.Duration(c.offset.Load())
}

func (c *Clock) SetOffset(d time.Duration) { c.offset.Store(int64(d)) }

// Zone resolves a TZ value. An unknown zone falls back to UTC, same as libc.
func (c *Clock) Zone(tz string) *time.Location {
	if tz == "" {
		return c.localZone()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// In is Now in the zone TZ names.
func (c *Clock) In(tz string) time.Time { return c.Now().In(c.Zone(tz)) }

// Local preserves an IANA name when available, so restored clocks keep DST rules.
// Go's generic "Local" name is host-specific, so save its abbreviation and offset.
func (c *Clock) Local() (string, int) {
	loc := c.localZone()
	name, offset := c.Now().In(loc).Zone()
	if loc.String() != "Local" {
		name = loc.String()
	}
	return name, offset
}

func (c *Clock) localZone() *time.Location {
	if c == nil {
		return time.Local
	}
	if c.local.Load() == nil {
		c.local.CompareAndSwap(nil, time.Local)
	}
	return c.local.Load()
}

// SetLocal restores that zone. A snapshot is read on someone else's machine,
// so the saved session keeps the zone it was written in, not the reader's.
// An offset is enough when the name is not one the zone database knows — in
// the browser, time.Local is a bare "UTC+3:30" to begin with.
func (c *Clock) SetLocal(name string, offset int) {
	if name != "Local" && name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			c.local.Store(loc)
			return
		}
	}
	c.local.Store(time.FixedZone(name, offset))
}
