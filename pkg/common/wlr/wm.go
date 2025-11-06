package wlr

import (
	"log/slog"
	"sync"
	"time"

	"github.com/neurlang/wayland/wl"
	"github.com/neurlang/wayland/wlclient"
)

var (
	registry *wl.Registry
	display  *wl.Display
	seat     []*wl.Seat
)

type windowmap map[wl.ProxyId]*Window

var windows = make(windowmap)

var IsRunning = false

func Windows() windowmap {
	return windows
}

func Activate(id wl.ProxyId) error {
	err := windows[id].Toplevel.Activate(seat[len(seat)-1])
	if err != nil {
		return err
	}

	return nil
}

var (
	IsSetup bool
	mu      sync.Mutex
)

func Init() {
	mu.Lock()
	defer mu.Unlock()

	if IsSetup {
		return
	}

	IsSetup = true
	count := 0

	for count < 10 {
		if err := start(); err != nil {
			slog.Error("windows", "init", err)
			slog.Info("windows", "setup", "retrying initWindowManager")
			count++
			IsSetup = false
			time.Sleep(1 * time.Second)
		}
	}

	slog.Error("windows", "init", "couldn't init window manager")
}

func start() error {
	var err error

	display, err = wl.Connect("")
	if err != nil {
		return err
	}

	display.AddErrorHandler(displayErrorHandler{})

	registry, err = display.GetRegistry()
	if err != nil {
		return err
	}

	registry.AddGlobalHandler(registryGlobalHandler{})

	_ = wlclient.DisplayRoundtrip(display)

	IsRunning = true

	for {
		err = display.Context().Run()
		if err != nil {
			return err
		}
	}
}

type displayErrorHandler struct{}

func (displayErrorHandler) HandleDisplayError(e wl.DisplayErrorEvent) {
	slog.Error("wm", "display error event: %v", e)
}

type registryGlobalHandler struct{}

func (registryGlobalHandler) HandleRegistryGlobal(e wl.RegistryGlobalEvent) {
	switch e.Interface {
	case "zwlr_foreign_toplevel_manager_v1":
		manager := NewZwlrForeignToplevelManagerV1(display.Context())

		err := registry.Bind(e.Name, e.Interface, e.Version, manager)
		if err != nil {
			slog.Error("wm", "unable to bind wl_compositor interface: %v", e)
		}

		manager.AddToplevelHandler(&Window{})
	case "wl_seat":
		seat = append(seat, wl.NewSeat(display.Context()))

		err := registry.Bind(e.Name, e.Interface, e.Version, seat[len(seat)-1])
		if err != nil {
			slog.Error("wm", "unable to bind wl_seat interface: %v", e)
		}
	}
}

type Window struct {
	mutex      sync.Mutex
	Toplevel   *ZwlrForeignToplevelHandleV1
	AppID      string
	Title      string
	AddChan    chan string
	DeleteChan chan string
}

func (*Window) HandleZwlrForeignToplevelManagerV1Toplevel(e ZwlrForeignToplevelManagerV1ToplevelEvent) {
	handler := &Window{
		Toplevel:   e.Toplevel,
		AddChan:    addChan,
		DeleteChan: deleteChan,
	}

	e.Toplevel.AddTitleHandler(handler)
	e.Toplevel.AddAppIdHandler(handler)
	e.Toplevel.AddClosedHandler(handler)

	windows[e.Toplevel.Id()] = &Window{Toplevel: e.Toplevel}
}

func (h *Window) HandleZwlrForeignToplevelHandleV1Closed(e ZwlrForeignToplevelHandleV1ClosedEvent) {
	if h.DeleteChan != nil {
		h.DeleteChan <- h.AppID
	}

	h.mutex.Lock()
	defer h.mutex.Unlock()
	delete(windows, h.Toplevel.Id())
}

func (h *Window) HandleZwlrForeignToplevelHandleV1AppId(e ZwlrForeignToplevelHandleV1AppIdEvent) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	windows[h.Toplevel.Id()].AppID = e.AppId
	h.AppID = e.AppId

	if h.AddChan != nil {
		h.AddChan <- e.AppId
	}
}

func (h *Window) HandleZwlrForeignToplevelHandleV1Title(e ZwlrForeignToplevelHandleV1TitleEvent) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	windows[h.Toplevel.Id()].Title = e.Title
}
