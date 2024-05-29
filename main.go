package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"strings"
	"strconv"

	"golang.org/x/sys/unix"
)

type Scope string

const (
	ScopeFull Scope = "full"
	ScopeSome Scope = "some"
)

type Trigger struct {
	Threshold time.Duration // in microseconds
	Window    time.Duration // in microseconds
	Scope     Scope
}

type Resource string

const (
	ResourceCPU    Resource = "cpu"
	ResourceIO     Resource = "io"
	ResourceMemory Resource = "memory"
)

type PressureStallInformation struct {
	resource Resource
	readFD   *os.File
	triggers []Trigger
}

type Result struct {
	Scope Scope
	Total int
	Avg10 float64
	Avg60 float64
	Avg300 float64
}	
	

func NewPSI(resource Resource) (*PressureStallInformation, error) {
	res := &PressureStallInformation{resource: resource}
	fd, err := res.open()
	if err != nil {
		return nil, err
	}
	res.readFD = fd
	return res, nil
}

func (p *PressureStallInformation) openPoll() (*os.File, error) {
	return os.OpenFile(fmt.Sprintf("/proc/pressure/%s", p.resource), syscall.O_RDWR|syscall.O_NONBLOCK, 0)
}

func (p *PressureStallInformation) open() (*os.File, error) {
	return os.OpenFile(fmt.Sprintf("/proc/pressure/%s", p.resource), syscall.O_RDONLY, 0)
}

func parsePressureEntry(entry string) (Result, error) {
	parts := strings.Split(entry, " ")
	if len(parts) < 5 {
		return Result{}, fmt.Errorf("invalid entry")
	}

	r := Result{}
	if parts[0] == "some" {
		r.Scope = ScopeSome
	} else {
		r.Scope = ScopeFull
	}

	avg10 := strings.Split(parts[1], "=")[1]
	avg60 := strings.Split(parts[2], "=")[1]
	avg300 := strings.Split(parts[3], "=")[1]
	total := strings.Split(parts[4], "=")[1]

	r.Avg10, _ = strconv.ParseFloat(avg10, 64)
	r.Avg60, _ = strconv.ParseFloat(avg60, 64)
	r.Avg300, _ = strconv.ParseFloat(avg300, 64)
	r.Total, _ = strconv.Atoi(total)

	return r, nil
}

func (p *PressureStallInformation) String() (string, error) {
	buf := make([]byte, 1024)
	n, err := p.readFD.Read(buf)
	if err != nil {
		return "", err
	}
	p.readFD.Seek(0, 0)
	res := string(buf[:n])
	for _, line := range strings.Split(res, "\n") {
		if line == "" {
			// skip empty line
			continue
		}
		r, err := parsePressureEntry(line)
		if err != nil {
			return "", err
		}
		fmt.Println(r)
	}
	return "", nil
}

type Ticker struct {
	C <-chan struct{}
}

func setTrigger(fd *os.File, trigger Trigger) error {
	if _, err := fd.Write([]byte(trigger.String())); err != nil {
		return err
	}
	return nil
}

func (p *PressureStallInformation) NewTicker(trigger Trigger) (*Ticker, error) {

	// new fd for each trigger for polling purposes instead of read only fd
	f, err := p.openPoll()
	if err != nil {
		return nil, err
	}

	if err := setTrigger(f, trigger); err != nil {
		return nil, err
	}

	var ch = make(chan struct{})
	go func() {
		for {
			n, err := unix.Poll([]unix.PollFd{
				{
					Fd:     int32(f.Fd()),
					Events: unix.POLLPRI,
				},
			}, -1)
			if err != nil {
				fmt.Println("error: " + err.Error())
				continue
			}
			switch {
			case n < 0:
				// todo: make this a proper error
				fmt.Println("error")
				continue
			case n != 1:
				// todo: make this a proper error
				fmt.Println("error")
				continue
			default:
				ch <- struct{}{}
			}
		}
	}()
	return &Ticker{C: ch}, nil
}

func (t Trigger) String() string {
	return fmt.Sprintf("%s %d %d\n", t.Scope, t.Threshold.Microseconds(), t.Window.Microseconds())
}

func main() {
	cpu, err := NewPSI(ResourceCPU)
	if err != nil {
		panic(err)
	}

	tick, err := cpu.NewTicker(Trigger{Scope: ScopeSome, Threshold: 100 * time.Millisecond, Window: 1 * time.Second})
	if err != nil {
		panic(err)
	}
	fmt.Println("CPU pressure tracking")
	for {
		select {
		case <-tick.C:
			cpu.String()
		}
	}
}
