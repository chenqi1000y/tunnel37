//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	appTitle          = "橘子本地代理"
	appVersion        = "1.1.0"
	defaultTunnelAddr = "tunnel.ma37.com:9081"
	defaultDemoBase   = "http://tunnel.ma37.com:18080"
	defaultToken      = "c_t5hzOcXiGR9_nGQTBw0LaAHfV6S57N"
	timerID           = 1

	className = "JuziLocalProxyDesktopWindow"

	wmCreate         = 0x0001
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmCommand        = 0x0111
	wmCtlColorStatic = 0x0138
	wmCtlColorEdit   = 0x0133
	wmTimer          = 0x0113
	wmSetFont        = 0x0030
	wmSize           = 0x0005
	wmAppTray        = 0x8001

	wsOverlapped  = 0x00000000
	wsVisible     = 0x10000000
	wsChild       = 0x40000000
	wsTabStop     = 0x00010000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsClipChildren = 0x02000000
	wsBorder      = 0x00800000
	wsVScroll     = 0x00200000

	wsExClientEdge = 0x00000200

	esAutoHScroll = 0x0080
	esReadOnly    = 0x0800
	esMultiLine   = 0x0004
	esAutoVScroll = 0x0040

	bnClicked = 0

	swHide = 0
	swShow = 5
	swRestore = 9

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	cwUseDefault = 0x80000000

	colorWindow   = 5
	defaultGUIFont = 17
	fwNormal      = 400
	fwBold        = 700
	transparent   = 1

	idcArrow       = 32512
	idiApplication = 32512

	mbIconInfo    = 0x00000040
	mbIconError   = 0x00000010
	mbIconWarning = 0x00000030
	mbOK          = 0x00000000

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	sizeMinimized = 1

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	mfString   = 0x00000000
	mfSeparator = 0x00000800
	tpmBottomAlign = 0x0020
	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002

	dwmaUseImmersiveDarkMode   = 20
	dwmaWindowCornerPreference = 33
	dwmaSystemBackdropType     = 38
	dwmwcpRound                = 2
	dwmsbtMainWindow           = 2
)

const (
	idAgentNameEdit = 1001
	idStartBtn      = 1002
	idStopBtn       = 1003
	idTestBtn       = 1004
	idCopyBtn       = 1005
	idStatusBtn     = 1006
	idUpdateBtn     = 1007
	idStatusTip     = 1008

	idTrayShow = 2001
	idTrayStart = 2002
	idTrayStop = 2003
	idTrayCopy = 2004
	idTrayExit = 2005
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procLoadIconW             = user32.NewProc("LoadIconW")
	procShowWindow            = user32.NewProc("ShowWindow")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetWindowTextW        = user32.NewProc("SetWindowTextW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procEnableWindow          = user32.NewProc("EnableWindow")
	procMessageBoxW           = user32.NewProc("MessageBoxW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenuW           = user32.NewProc("AppendMenuW")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procOpenClipboard         = user32.NewProc("OpenClipboard")
	procCloseClipboard        = user32.NewProc("CloseClipboard")
	procEmptyClipboard        = user32.NewProc("EmptyClipboard")
	procSetClipboardData      = user32.NewProc("SetClipboardData")
	procSetBkMode             = gdi32.NewProc("SetBkMode")
	procSetTextColor          = gdi32.NewProc("SetTextColor")
	procCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procGetStockObject        = gdi32.NewProc("GetStockObject")
	procCreateFontW           = gdi32.NewProc("CreateFontW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc           = kernel32.NewProc("GlobalAlloc")
	procGlobalLock            = kernel32.NewProc("GlobalLock")
	procGlobalUnlock          = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory         = kernel32.NewProc("RtlMoveMemory")
	procShellNotifyIconW      = shell32.NewProc("Shell_NotifyIconW")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	proxyIDPattern = regexp.MustCompile(`proxy_id=([0-9a-fA-F-]{36})`)

	mainApp *desktopApp
	mainUI  *desktopUI
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	LPrivate uint32
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type desktopApp struct {
	mu         sync.RWMutex
	agentPath  string
	tunnelAddr string
	agentName  string
	token      string
	demoBase   string

	proc      *exec.Cmd
	stopping  bool
	status    string
	statusTip string
	proxyID   string
	lastTest  string
	updateTip string
	startedAt time.Time
	logs      []string
	cfg       desktopConfig
}

type desktopUI struct {
	hwnd uintptr

	bgBrush     uintptr
	panelBrush  uintptr
	statusBrush uintptr

	titleFont uintptr
	textFont  uintptr
	logFont   uintptr

	titleLabel      uintptr
	statusValue     uintptr
	statusTipValue  uintptr
	agentNameEdit   uintptr
	proxyIDEdit     uintptr
	uptimeValue     uintptr
	lastTestValue   uintptr
	versionValue    uintptr
	updateValue     uintptr
	statusLinkValue uintptr
	logEdit         uintptr
	startBtn        uintptr
	stopBtn         uintptr
	testBtn         uintptr
	copyBtn         uintptr
	statusBtn       uintptr
	updateBtn       uintptr
	trayAdded       bool
	trayIcon        uintptr
}

type desktopConfig struct {
	AgentName string `json:"agent_name,omitempty"`
}

func main() {
	mainApp = newDesktopApp()
	mainUI = &desktopUI{}
	if err := runDesktop(); err != nil {
		showMessage(0, "启动失败", err.Error(), mbIconError)
	}
}

func newDesktopApp() *desktopApp {
	agentPath := defaultAgentPath()
	cfg := loadDesktopConfig(agentPath)
	agentName := firstNonEmpty(cfg.AgentName, hostnameOr("windows-agent"))
	app := &desktopApp{
		agentPath:  agentPath,
		tunnelAddr: defaultTunnelAddr,
		agentName:  agentName,
		token:      defaultToken,
		demoBase:   defaultDemoBase,
		status:     "未启动",
		statusTip:  "红色表示未连接或已中断",
		lastTest:   "尚未测试",
		updateTip:  "当前版本 " + appVersion,
		logs:       make([]string, 0, 160),
		cfg:        cfg,
	}
	if id := loadPersistedProxyID(agentPath); id != "" {
		app.proxyID = id
		app.log("检测到历史代理ID，后续启动会优先复用: " + id)
	} else {
		app.log("首次使用时会自动生成代理ID。")
	}
	app.log("启动成功后，窗口状态会变为绿色。")
	app.log("如果连接中断，日志会提示“代理中断，正在重连”。")
	return app
}

func runDesktop() error {
	instance, _, _ := procGetModuleHandleW.Call(0)
	bgBrush, _, _ := procCreateSolidBrush.Call(colorRef(245, 247, 252))
	panelBrush, _, _ := procCreateSolidBrush.Call(colorRef(255, 255, 255))
	statusBrush, _, _ := procCreateSolidBrush.Call(colorRef(245, 247, 252))
	mainUI.bgBrush = bgBrush
	mainUI.panelBrush = panelBrush
	mainUI.statusBrush = statusBrush

	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		Style:      csHRedraw | csVRedraw,
		WndProc:    syscall.NewCallback(windowProc),
		Instance:   instance,
		Icon:       mustLoadIcon(),
		Cursor:     mustLoadCursor(),
		Background: bgBrush,
		ClassName:  utf16Ptr(className),
		IconSm:     mustLoadIcon(),
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return fmt.Errorf("注册窗口类失败: %v", err)
	}

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren | wsVisible)
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(appTitle))),
		style,
		cwUseDefault, cwUseDefault,
		880, 620,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("创建主窗口失败: %v", err)
	}
	mainUI.hwnd = hwnd
	applyWindowEffects(hwnd)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == -1 {
			return fmt.Errorf("消息循环失败")
		}
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		mainUI.hwnd = hwnd
		mainUI.initControls()
		mainUI.syncFromApp()
		procSetTimer.Call(hwnd, timerID, 1000, 0)
		return 0
	case wmCommand:
		switch loword(wParam) {
		case idTrayShow:
			mainUI.restoreFromTray()
			return 0
		case idTrayStart:
			go func() {
				if err := mainApp.start(); err != nil {
					showMessage(hwnd, "启动失败", err.Error(), mbIconError)
				}
			}()
			return 0
		case idTrayStop:
			go mainApp.stop()
			return 0
		case idTrayCopy:
			if err := copyTextToClipboard(hwnd, mainApp.currentProxyID()); err != nil {
				showMessage(hwnd, "复制失败", err.Error(), mbIconError)
			}
			return 0
		case idTrayExit:
			go mainApp.stop()
			procDestroyWindow.Call(hwnd)
			return 0
		}
		if hiword(wParam) == bnClicked {
			switch loword(wParam) {
			case idStartBtn:
				go func() {
					if err := mainApp.start(); err != nil {
						showMessage(hwnd, "启动失败", err.Error(), mbIconError)
					}
				}()
			case idStopBtn:
				go mainApp.stop()
			case idTestBtn:
				go func() {
					msg := mainApp.test()
					showMessage(hwnd, "测试结果", msg, mbIconInfo)
				}()
			case idCopyBtn:
				if err := copyTextToClipboard(hwnd, mainApp.currentProxyID()); err != nil {
					showMessage(hwnd, "复制失败", err.Error(), mbIconError)
				} else {
					showMessage(hwnd, "复制成功", "代理ID 已复制。", mbIconInfo)
				}
			case idStatusBtn:
				if err := openURL(mainApp.statusLink()); err != nil {
					showMessage(hwnd, "打开失败", err.Error(), mbIconError)
				}
			case idUpdateBtn:
				go func() {
					msg := mainApp.checkUpdate()
					showMessage(hwnd, "更新检查", msg, mbIconInfo)
				}()
			}
		}
		return 0
	case wmSize:
		if wParam == sizeMinimized {
			mainUI.minimizeToTray()
			return 0
		}
	case wmAppTray:
		switch lParam {
		case wmLButtonDblClk:
			mainUI.restoreFromTray()
		case wmRButtonUp:
			mainUI.showTrayMenu()
		}
		return 0
	case wmTimer:
		mainUI.syncFromApp()
		return 0
	case wmCtlColorStatic:
		return mainUI.handleStaticColor(wParam, lParam)
	case wmCtlColorEdit:
		return mainUI.handleEditColor(wParam, lParam)
	case wmClose:
		go mainApp.stop()
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		mainUI.removeTray()
		procKillTimer.Call(hwnd, timerID)
		mainUI.dispose()
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func (ui *desktopUI) initControls() {
	ui.titleFont = mustCreateFont(30, fwBold, "Microsoft YaHei UI")
	ui.textFont = mustCreateFont(16, fwNormal, "Microsoft YaHei UI")
	ui.logFont = mustCreateFont(15, fwNormal, "Consolas")

	ui.titleLabel = ui.createLabel("橘子本地代理", 26, 18, 260, 36, ui.titleFont)
	ui.createLabel("启动成功为绿色，未连接或中断为红色。历史代理ID会自动复用。", 28, 58, 560, 24, ui.textFont)

	ui.createLabel("当前状态", 28, 104, 92, 24, ui.textFont)
	ui.statusValue = ui.createLabel("● 未启动", 130, 102, 260, 28, ui.textFont)
	ui.statusTipValue = ui.createLabel("等待启动", 320, 104, 500, 24, ui.textFont)

	ui.createLabel("Agent 名称", 28, 146, 92, 24, ui.textFont)
	ui.agentNameEdit = ui.createEdit(mainApp.agentName, 130, 142, 280, 30, false, idAgentNameEdit)

	ui.createLabel("代理ID", 28, 188, 92, 24, ui.textFont)
	ui.proxyIDEdit = ui.createEdit("", 130, 184, 690, 32, true, 0)

	ui.createLabel("运行时长", 28, 232, 92, 24, ui.textFont)
	ui.uptimeValue = ui.createLabel("00:00:00", 130, 230, 160, 24, ui.textFont)

	ui.createLabel("代理测试", 320, 232, 92, 24, ui.textFont)
	ui.lastTestValue = ui.createLabel("尚未测试", 412, 230, 250, 24, ui.textFont)

	ui.createLabel("版本", 28, 272, 92, 24, ui.textFont)
	ui.versionValue = ui.createLabel("当前版本 "+appVersion, 130, 270, 280, 24, ui.textFont)

	ui.createLabel("更新状态", 430, 272, 92, 24, ui.textFont)
	ui.updateValue = ui.createLabel("尚未检查更新", 522, 270, 300, 24, ui.textFont)

	ui.createLabel("状态页", 28, 312, 92, 24, ui.textFont)
	ui.statusLinkValue = ui.createLabel("-", 130, 310, 690, 24, ui.textFont)

	ui.startBtn = ui.createButton("启动代理", idStartBtn, 28, 356, 126, 40)
	ui.stopBtn = ui.createButton("停止代理", idStopBtn, 168, 356, 126, 40)
	ui.testBtn = ui.createButton("测试代理", idTestBtn, 308, 356, 126, 40)
	ui.copyBtn = ui.createButton("复制代理ID", idCopyBtn, 448, 356, 138, 40)
	ui.statusBtn = ui.createButton("打开状态页", idStatusBtn, 600, 356, 126, 40)
	ui.updateBtn = ui.createButton("检查更新", idUpdateBtn, 740, 356, 80, 40)

	ui.createLabel("最近动态", 28, 414, 100, 24, ui.textFont)
	ui.logEdit = ui.createLogEdit(28, 444, 792, 132)
}

func (ui *desktopUI) syncFromApp() {
	snap := mainApp.snapshot()
	setWindowText(ui.statusValue, "● "+snap.status)
	setWindowText(ui.statusTipValue, snap.statusTip)
	setWindowText(ui.proxyIDEdit, snap.proxyID)
	setWindowText(ui.uptimeValue, snap.uptime)
	setWindowText(ui.lastTestValue, snap.lastTest)
	setWindowText(ui.versionValue, "当前版本 "+appVersion)
	setWindowText(ui.updateValue, snap.updateTip)
	setWindowText(ui.statusLinkValue, snap.statusLink)

	name := windowText(ui.agentNameEdit)
	if strings.TrimSpace(name) == "" || name == mainApp.agentName {
		setWindowText(ui.agentNameEdit, snap.agentName)
	}
	setWindowText(ui.logEdit, strings.Join(snap.logs, "\r\n"))

	enableWindow(ui.startBtn, !snap.running)
	enableWindow(ui.stopBtn, snap.running || strings.Contains(snap.status, "中断"))
	enableWindow(ui.testBtn, strings.TrimSpace(snap.proxyID) != "")
	enableWindow(ui.copyBtn, strings.TrimSpace(snap.proxyID) != "")
	enableWindow(ui.statusBtn, strings.TrimSpace(snap.proxyID) != "")
}

func (ui *desktopUI) handleStaticColor(wParam, lParam uintptr) uintptr {
	procSetBkMode.Call(wParam, transparent)
	handle := lParam
	switch handle {
	case ui.statusValue:
		procSetTextColor.Call(wParam, statusColorRef(mainApp.currentStatus()))
	default:
		procSetTextColor.Call(wParam, colorRef(44, 54, 74))
	}
	return ui.bgBrush
}

func (ui *desktopUI) handleEditColor(wParam, lParam uintptr) uintptr {
	_ = lParam
	procSetBkMode.Call(wParam, transparent)
	procSetTextColor.Call(wParam, colorRef(35, 43, 60))
	return ui.panelBrush
}

func (ui *desktopUI) dispose() {
	for _, obj := range []uintptr{ui.titleFont, ui.textFont, ui.logFont, ui.bgBrush, ui.panelBrush, ui.statusBrush} {
		if obj != 0 {
			procDeleteObject.Call(obj)
		}
	}
}

func (ui *desktopUI) createLabel(text string, x, y, w, h int32, font uintptr) uintptr {
	hwnd := createWindow(wsChild|wsVisible, 0, "STATIC", text, x, y, w, h, ui.hwnd, 0)
	sendFont(hwnd, font)
	return hwnd
}

func (ui *desktopUI) createEdit(text string, x, y, w, h int32, readOnly bool, id uint16) uintptr {
	style := uint32(wsChild | wsVisible | wsTabStop | wsBorder | esAutoHScroll)
	if readOnly {
		style |= esReadOnly
	}
	hwnd := createWindow(style, wsExClientEdge, "EDIT", text, x, y, w, h, ui.hwnd, id)
	sendFont(hwnd, ui.textFont)
	return hwnd
}

func (ui *desktopUI) createLogEdit(x, y, w, h int32) uintptr {
	style := uint32(wsChild | wsVisible | wsVScroll | wsBorder | esMultiLine | esAutoVScroll | esReadOnly)
	hwnd := createWindow(style, wsExClientEdge, "EDIT", "", x, y, w, h, ui.hwnd, 0)
	sendFont(hwnd, ui.logFont)
	return hwnd
}

func (ui *desktopUI) createButton(text string, id uint16, x, y, w, h int32) uintptr {
	hwnd := createWindow(wsChild|wsVisible|wsTabStop, 0, "BUTTON", text, x, y, w, h, ui.hwnd, id)
	sendFont(hwnd, ui.textFont)
	return hwnd
}

func (a *desktopApp) start() error {
	a.mu.Lock()
	if a.proc != nil && a.proc.Process != nil {
		a.mu.Unlock()
		return fmt.Errorf("代理已在运行中")
	}
	name := strings.TrimSpace(windowText(mainUI.agentNameEdit))
	if name == "" {
		name = hostnameOr("windows-agent")
	}
	a.agentName = name
	a.cfg.AgentName = name
	a.status = "启动中"
	a.statusTip = "正在连接服务器"
	a.lastTest = "尚未测试"
	a.stopping = false
	if a.proxyID == "" {
		a.proxyID = loadPersistedProxyID(a.agentPath)
	}
	a.startedAt = time.Now()
	a.mu.Unlock()
	_ = saveDesktopConfig(a.agentPath, a.cfg)

	if _, err := os.Stat(a.agentPath); err != nil {
		a.setStatus("启动失败")
		return fmt.Errorf("未找到 tunnel-agent.exe: %s", a.agentPath)
	}
	if err := a.ensureReusableProxyState(); err != nil {
		a.log("旧代理ID准备失败: " + err.Error())
	}

	cmd := exec.Command(a.agentPath, "-tunnel", a.tunnelAddr, "-name", a.agentName, "-token", a.token)
	cmd.Dir = filepath.Dir(a.agentPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.setStatus("启动失败")
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		a.setStatus("启动失败")
		return err
	}
	if err := cmd.Start(); err != nil {
		a.setStatus("启动失败")
		return err
	}

	a.mu.Lock()
	a.proc = cmd
	a.mu.Unlock()

	a.log("已启动本地代理进程。")
	a.log("Agent 名称: " + a.agentName)
	if id := loadPersistedProxyID(a.agentPath); id != "" {
		a.log("本地发现历史代理ID，准备优先复用: " + id)
	}

	go a.readPipe(stdout, false)
	go a.readPipe(stderr, true)
	go a.waitExit(cmd)
	return nil
}

func (a *desktopApp) ensureReusableProxyState() error {
	current := loadPersistedProxyID(a.agentPath)
	if current != "" {
		a.mu.Lock()
		if a.proxyID == "" {
			a.proxyID = current
		}
		a.mu.Unlock()
		return nil
	}

	target := strings.TrimRight(a.demoBase, "/") + "/api/demo/tunnel/agents"
	client := http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var payload struct {
		Data struct {
			Items []struct {
				ProxyID   string `json:"proxy_id"`
				AgentName string `json:"agent_name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}

	var matched string
	for _, item := range payload.Data.Items {
		if strings.EqualFold(strings.TrimSpace(item.AgentName), strings.TrimSpace(a.agentName)) && strings.TrimSpace(item.ProxyID) != "" {
			matched = strings.TrimSpace(item.ProxyID)
			break
		}
	}
	if matched == "" {
		return nil
	}
	if err := writePersistedProxyID(a.agentPath, a.agentName, matched); err != nil {
		return err
	}
	a.mu.Lock()
	a.proxyID = matched
	a.mu.Unlock()
	a.log("已根据 Agent 名称找回旧代理ID: " + matched)
	return nil
}

func (a *desktopApp) stop() {
	a.mu.Lock()
	cmd := a.proc
	a.stopping = true
	a.proc = nil
	a.status = "未启动"
	a.statusTip = "代理已停止"
	a.startedAt = time.Time{}
	a.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	a.log("已停止本地代理。")
}

func (a *desktopApp) waitExit(cmd *exec.Cmd) {
	err := cmd.Wait()
	a.mu.Lock()
	intentional := a.stopping
	if a.proc == cmd {
		a.proc = nil
	}
	if !intentional {
		a.status = "代理中断"
		a.statusTip = "连接已断开，需重新启动"
		a.startedAt = time.Time{}
	}
	a.stopping = false
	a.mu.Unlock()

	if intentional {
		a.log("代理进程已正常停止。")
		return
	}
	if err != nil {
		a.log("代理已中断: " + err.Error())
	} else {
		a.log("代理已中断，进程已退出。")
	}
}

func (a *desktopApp) test() string {
	a.mu.RLock()
	proxyID := strings.TrimSpace(a.proxyID)
	base := a.demoBase
	a.mu.RUnlock()
	if proxyID == "" {
		a.log("测试失败：当前还没有代理ID。")
		return "当前没有代理ID，无法测试。"
	}

	target := base + "/api/demo/tunnel/test?proxy_id=" + url.QueryEscape(proxyID)
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		a.mu.Lock()
		a.lastTest = "测试失败"
		a.mu.Unlock()
		a.log("测试失败: " + err.Error())
		return "代理测试失败：" + err.Error()
	}
	defer resp.Body.Close()

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			ExitIP  string `json:"exit_ip"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		a.mu.Lock()
		a.lastTest = "测试失败"
		a.mu.Unlock()
		a.log("测试失败: " + err.Error())
		return "测试结果解析失败：" + err.Error()
	}

	if payload.Data.Success {
		msg := "测试成功，出口 IP: " + firstNonEmpty(payload.Data.ExitIP, "-")
		a.mu.Lock()
		a.lastTest = "测试成功"
		a.mu.Unlock()
		a.log(msg)
		return msg
	}
	message := firstNonEmpty(payload.Data.Message, payload.Message)
	a.mu.Lock()
	a.lastTest = "测试失败"
	a.mu.Unlock()
	a.log("测试失败: " + message)
	return "代理测试失败：" + firstNonEmpty(message, "未知错误")
}

func (a *desktopApp) checkUpdate() string {
	target := a.demoBase + "/api/demo/app/juzi/update"
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		a.log("检查更新失败: " + err.Error())
		return "检查更新失败：" + err.Error()
	}
	defer resp.Body.Close()

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Version     string `json:"version"`
			ForceUpdate bool   `json:"force_update"`
			Notice      string `json:"notice"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		a.log("检查更新失败: " + err.Error())
		return "更新信息解析失败：" + err.Error()
	}

	msg := "当前已是最新版本"
	updateTip := "当前版本 " + appVersion
	if strings.TrimSpace(payload.Data.Version) != "" {
		if payload.Data.Version != appVersion {
			msg = "发现新版本 " + payload.Data.Version
			updateTip = "发现新版本 " + payload.Data.Version
		}
		if payload.Data.ForceUpdate {
			msg += "，服务器要求强制更新"
			updateTip += "（强制）"
		}
	}
	if strings.TrimSpace(payload.Data.Notice) != "" {
		msg += "\n" + payload.Data.Notice
	}
	a.mu.Lock()
	a.updateTip = updateTip
	a.mu.Unlock()
	a.log(msg)
	return msg
}

func (a *desktopApp) readPipe(r io.ReadCloser, isErr bool) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if isErr {
			a.log("错误: " + line)
			continue
		}
		a.log(line)
		if m := proxyIDPattern.FindStringSubmatch(line); len(m) == 2 {
			a.mu.Lock()
			a.proxyID = m[1]
			if a.status != "代理中断" {
				a.status = "已启动"
			}
			a.statusTip = "代理已连接，可直接提交代理ID"
			if a.startedAt.IsZero() {
				a.startedAt = time.Now()
			}
			a.mu.Unlock()
		}
		if strings.Contains(line, "注册成功") || strings.Contains(line, "收到 pong") {
			a.setStatus("已启动")
		}
		if strings.Contains(line, "连接中断") {
			a.mu.Lock()
			a.status = "代理中断"
			a.statusTip = "连接中断，正在重连"
			a.mu.Unlock()
			a.log("代理中断，正在重连。")
		}
	}
}

func (a *desktopApp) setStatus(status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = status
	switch status {
	case "已启动":
		a.statusTip = "代理已连接，可直接提交代理ID"
	case "启动中":
		a.statusTip = "正在连接服务器"
	case "代理中断":
		a.statusTip = "连接中断，正在重连"
	default:
		a.statusTip = status
	}
}

func (a *desktopApp) statusLink() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.proxyID == "" {
		return "-"
	}
	short := a.proxyID
	if len(short) > 8 {
		short = short[:8]
	}
	return a.demoBase + "/t/" + short
}

func (a *desktopApp) currentProxyID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return strings.TrimSpace(a.proxyID)
}

func (a *desktopApp) currentStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

func (a *desktopApp) log(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message)
	a.logs = append(a.logs, line)
	if len(a.logs) > 120 {
		a.logs = append([]string(nil), a.logs[len(a.logs)-120:]...)
	}
}

type appSnapshot struct {
	status     string
	statusTip  string
	proxyID    string
	lastTest   string
	uptime     string
	logs       []string
	agentName  string
	running    bool
	statusLink string
	updateTip  string
}

func (a *desktopApp) snapshot() appSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	logs := append([]string(nil), a.logs...)
	return appSnapshot{
		status:     a.status,
		statusTip:  a.statusTip,
		proxyID:    a.proxyID,
		lastTest:   a.lastTest,
		uptime:     formatDuration(a.startedAt, a.proc != nil || strings.Contains(a.status, "中断")),
		logs:       logs,
		agentName:  a.agentName,
		running:    a.proc != nil,
		statusLink: a.statusLink(),
		updateTip:  a.updateTip,
	}
}

func loadPersistedProxyID(agentPath string) string {
	path := filepath.Join(filepath.Dir(agentPath), "tunnel-agent.state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state struct {
		ProxyID string `json:"proxy_id"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return ""
	}
	return strings.TrimSpace(state.ProxyID)
}

func writePersistedProxyID(agentPath, agentName, proxyID string) error {
	path := filepath.Join(filepath.Dir(agentPath), "tunnel-agent.state.json")
	payload := struct {
		AgentID   string    `json:"agent_id,omitempty"`
		ProxyID   string    `json:"proxy_id,omitempty"`
		AgentName string    `json:"agent_name,omitempty"`
		UpdatedAt time.Time `json:"updated_at,omitempty"`
	}{
		ProxyID:   strings.TrimSpace(proxyID),
		AgentName: strings.TrimSpace(agentName),
		UpdatedAt: time.Now(),
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func formatDuration(startedAt time.Time, running bool) string {
	if startedAt.IsZero() || !running {
		return "00:00:00"
	}
	d := time.Since(startedAt).Round(time.Second)
	if d < 0 {
		d = 0
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func defaultAgentPath() string {
	exePath, err := os.Executable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return "tunnel-agent.exe"
	}
	return filepath.Join(filepath.Dir(exePath), "tunnel-agent.exe")
}

func hostnameOr(fallback string) string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func statusColorRef(status string) uintptr {
	switch {
	case strings.Contains(status, "已启动"):
		return colorRef(18, 148, 92)
	case strings.Contains(status, "启动中"):
		return colorRef(205, 130, 12)
	default:
		return colorRef(198, 65, 70)
	}
}

func colorRef(r, g, b byte) uintptr {
	return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16)
}

func loword(v uintptr) uint16 { return uint16(v & 0xffff) }
func hiword(v uintptr) uint16 { return uint16((v >> 16) & 0xffff) }

func utf16Ptr(s string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(s)
	return ptr
}

func sendFont(hwnd, font uintptr) {
	procSendMessageW.Call(hwnd, wmSetFont, font, 1)
}

func setWindowText(hwnd uintptr, text string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func enableWindow(hwnd uintptr, enabled bool) {
	var v uintptr
	if enabled {
		v = 1
	}
	procEnableWindow.Call(hwnd, v)
}

func createWindow(style uint32, exStyle uint32, className, text string, x, y, w, h int32, parent uintptr, id uint16) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent,
		uintptr(id),
		0,
		0,
	)
	return hwnd
}

func mustLoadCursor() uintptr {
	ret, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	return ret
}

func mustLoadIcon() uintptr {
	ret, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return ret
}

func mustCreateFont(height int32, weight int32, face string) uintptr {
	ret, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(utf16Ptr(face))),
	)
	if ret == 0 {
		ret, _, _ = procGetStockObject.Call(defaultGUIFont)
	}
	return ret
}

func applyWindowEffects(hwnd uintptr) {
	corner := uint32(dwmwcpRound)
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmaWindowCornerPreference),
		uintptr(unsafe.Pointer(&corner)),
		unsafe.Sizeof(corner),
	)
	backdrop := uint32(dwmsbtMainWindow)
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmaSystemBackdropType),
		uintptr(unsafe.Pointer(&backdrop)),
		unsafe.Sizeof(backdrop),
	)
	dark := int32(0)
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&dark)),
		unsafe.Sizeof(dark),
	)
}

func (ui *desktopUI) minimizeToTray() {
	if ui.trayAdded {
		procShowWindow.Call(ui.hwnd, swHide)
		return
	}
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             ui.hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmAppTray,
		HIcon:            mustLoadIcon(),
	}
	copyUTF16(nid.SzTip[:], "橘子本地代理")
	if ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ret != 0 {
		ui.trayAdded = true
		ui.trayIcon = nid.HIcon
		procShowWindow.Call(ui.hwnd, swHide)
		mainApp.log("已最小化到系统托盘。")
	}
}

func (ui *desktopUI) removeTray() {
	if !ui.trayAdded {
		return
	}
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   ui.hwnd,
		UID:    1,
	}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	ui.trayAdded = false
}

func (ui *desktopUI) restoreFromTray() {
	ui.removeTray()
	procShowWindow.Call(ui.hwnd, swRestore)
	procSetForegroundWindow.Call(ui.hwnd)
}

func (ui *desktopUI) showTrayMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	appendMenu(menu, idTrayShow, "打开主界面")
	appendMenu(menu, idTrayStart, "启动代理")
	appendMenu(menu, idTrayStop, "停止代理")
	appendMenu(menu, idTrayCopy, "复制代理ID")
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendMenu(menu, idTrayExit, "退出应用")
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(ui.hwnd)
	procTrackPopupMenu.Call(menu, tpmLeftAlign|tpmBottomAlign|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, ui.hwnd, 0)
}

func appendMenu(menu uintptr, id uint16, text string) {
	procAppendMenuW.Call(menu, mfString, uintptr(id), uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func copyUTF16(dst []uint16, text string) {
	src, _ := syscall.UTF16FromString(text)
	n := len(src)
	if n > len(dst) {
		n = len(dst)
	}
	copy(dst[:n], src[:n])
}

func loadDesktopConfig(agentPath string) desktopConfig {
	path := desktopConfigPath(agentPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return desktopConfig{}
	}
	var cfg desktopConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return desktopConfig{}
	}
	return cfg
}

func saveDesktopConfig(agentPath string, cfg desktopConfig) error {
	path := desktopConfigPath(agentPath)
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func desktopConfigPath(agentPath string) string {
	return filepath.Join(filepath.Dir(agentPath), "juzi-desktop.json")
}

func showMessage(hwnd uintptr, title, text string, flags uintptr) {
	procMessageBoxW.Call(
		hwnd,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		flags|mbOK,
	)
}

func copyTextToClipboard(hwnd uintptr, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("当前还没有代理ID")
	}
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	if ret, _, _ := procOpenClipboard.Call(hwnd); ret == 0 {
		return fmt.Errorf("无法打开剪贴板")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	size := uintptr(len(utf16) * 2)
	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return fmt.Errorf("分配剪贴板内存失败")
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("锁定剪贴板内存失败")
	}
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size)
	procGlobalUnlock.Call(hMem)
	if ret, _, _ := procSetClipboardData.Call(cfUnicodeText, hMem); ret == 0 {
		return fmt.Errorf("写入剪贴板失败")
	}
	return nil
}

func openURL(target string) error {
	if strings.TrimSpace(target) == "" || target == "-" {
		return fmt.Errorf("当前还没有状态页链接")
	}
	cmd := exec.Command("cmd", "/c", "start", "", target)
	return cmd.Start()
}
