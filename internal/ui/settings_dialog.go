package ui

import (
	"fmt"
	"strconv"
	"strings"

	"ghost-downloader-go-win32/internal/config"
	httpdownload "ghost-downloader-go-win32/internal/download/http"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type btSettingsDialogValues struct {
	listenPortText            string
	metadataTimeoutText       string
	connectionsLimitText      string
	downloadRateLimitKiBText  string
	uploadRateLimitKiBText    string
	seedRatioLimitPercentText string
	seedTimeLimitMinutesText  string
	trackersText              string
	sequentialDownload        bool
	enableDHT                 bool
	enableLSD                 bool
	enableUPnP                bool
	enableNATPMP              bool
	saveMagnetTorrentFile     bool
}

const (
	settingsDialogWidth     = 820
	settingsDialogHeight    = 720
	settingsDialogMinWidth  = 760
	settingsDialogMinHeight = 620
	settingsDialogTabCount  = 4
)

func runSettingsDialog(owner walk.Form, current config.Settings) (config.Settings, bool, error) {
	var dlg *walk.Dialog
	var acceptButton, cancelButton *walk.PushButton
	var downloadDirEdit, proxyEdit, blockEdit, maxConcurrentEdit, retryEdit, speedLimitEdit *walk.LineEdit
	var browserTokenEdit, browserPortEdit *walk.LineEdit
	var browserEnabledCheck *walk.CheckBox
	var ffmpegInstallDirEdit *walk.LineEdit
	var btListenPortEdit, btMetadataTimeoutEdit, btConnectionsLimitEdit, btDownloadRateEdit, btUploadRateEdit, btSeedRatioEdit, btSeedTimeEdit *walk.LineEdit
	var btSequentialCheck, btEnableDHTCheck, btEnableLSDCheck, btEnableUPnPCheck, btEnableNATPMPCheck, btSaveMagnetCheck *walk.CheckBox
	var btTrackersEdit *walk.TextEdit
	var m3u8InstallDirEdit, m3u8OutputFormatEdit, m3u8ThreadEdit, m3u8RetryEdit, m3u8TimeoutEdit, m3u8SubtitleFormatEdit *walk.LineEdit
	var m3u8ConcurrentCheck, m3u8CheckSegmentsCheck, m3u8DeleteAfterDoneCheck, m3u8SelectAllCheck, m3u8MP4DecryptCheck *walk.CheckBox
	var headersEdit, cookiesEdit *walk.TextEdit
	var themeModeBox *walk.ComboBox
	next := current

	dialog := Dialog{
		AssignTo:      &dlg,
		Title:         "设置",
		Size:          Size{Width: settingsDialogWidth, Height: settingsDialogHeight},
		MinSize:       Size{Width: settingsDialogMinWidth, Height: settingsDialogMinHeight},
		Layout:        VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
		DefaultButton: &acceptButton,
		CancelButton:  &cancelButton,
		Children: []Widget{
			TabWidget{
				StretchFactor: 1,
				Pages: []TabPage{
					{
						Title:  "常规",
						Layout: VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
						Children: []Widget{
							Label{Text: "外观"},
							Composite{
								Layout: Grid{Columns: 2},
								Children: []Widget{
									Label{Text: "主题"},
									ComboBox{AssignTo: &themeModeBox, Model: []string{"跟随系统", "浅色", "深色"}, CurrentIndex: themeModeIndex(current.ThemeMode)},
								},
							},
							Label{Text: "下载"},
							Composite{
								Layout: Grid{Columns: 3},
								Children: []Widget{
									Label{Text: "保存目录"},
									LineEdit{
										AssignTo:   &downloadDirEdit,
										Text:       current.DownloadDir,
										ColumnSpan: 1,
									},
									PushButton{
										Text: "浏览…",
										OnClicked: func() {
											dialog := new(walk.FileDialog)
											dialog.Title = "选择下载目录"
											if ok, err := dialog.ShowBrowseFolder(owner); err == nil && ok {
												downloadDirEdit.SetText(dialog.FilePath)
											}
										},
									},
									Label{Text: "代理地址"},
									LineEdit{
										AssignTo:   &proxyEdit,
										Text:       current.ProxyURL,
										ColumnSpan: 2,
									},
								},
							},
							Label{Text: "任务限制"},
							Composite{
								Layout: Grid{Columns: 2},
								Children: []Widget{
									Label{Text: "分块数"},
									LineEdit{AssignTo: &blockEdit, Text: strconv.Itoa(current.BlockNum)},
									Label{Text: "最大并发任务"},
									LineEdit{AssignTo: &maxConcurrentEdit, Text: strconv.Itoa(current.MaxConcurrent)},
									Label{Text: "重试次数"},
									LineEdit{AssignTo: &retryEdit, Text: strconv.Itoa(current.RetryCount)},
									Label{Text: "限速（KiB/s）"},
									LineEdit{AssignTo: &speedLimitEdit, Text: strconv.FormatInt(current.SpeedLimitKiB, 10)},
								},
							},
							Label{Text: "浏览器扩展"},
							Composite{
								Layout: Grid{Columns: 3},
								Children: []Widget{
									Label{Text: "启用"},
									CheckBox{
										AssignTo:   &browserEnabledCheck,
										Checked:    current.BrowserExtensionEnabled,
										ColumnSpan: 2,
									},
									Label{Text: "配对令牌"},
									LineEdit{
										AssignTo:   &browserTokenEdit,
										Text:       current.BrowserPairToken,
										ReadOnly:   true,
										ColumnSpan: 1,
									},
									PushButton{
										Text: "重新生成",
										OnClicked: func() {
											browserTokenEdit.SetText(config.NewBrowserPairToken())
										},
									},
									Label{Text: "端口"},
									LineEdit{AssignTo: &browserPortEdit, Text: strconv.Itoa(current.BrowserBridgePort)},
									Label{Text: "默认 14370"},
								},
							},
						},
					},
					{
						Title:  "BitTorrent",
						Layout: VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
						Children: []Widget{
							Label{Text: "BitTorrent"},
							Composite{
								Layout: Grid{Columns: 4},
								Children: []Widget{
									Label{Text: "监听端口"},
									LineEdit{AssignTo: &btListenPortEdit, Text: strconv.Itoa(current.BTListenPort)},
									Label{Text: "元数据超时（秒）"},
									LineEdit{AssignTo: &btMetadataTimeoutEdit, Text: strconv.Itoa(current.BTMetadataTimeoutSec)},
									Label{Text: "连接数"},
									LineEdit{AssignTo: &btConnectionsLimitEdit, Text: strconv.Itoa(current.BTConnectionsLimit)},
									Label{Text: "下载限速（KiB/s）"},
									LineEdit{AssignTo: &btDownloadRateEdit, Text: strconv.FormatInt(current.BTDownloadRateLimitKiB, 10)},
									Label{Text: "上传限速（KiB/s）"},
									LineEdit{AssignTo: &btUploadRateEdit, Text: strconv.FormatInt(current.BTUploadRateLimitKiB, 10)},
									Label{Text: "做种分享率（%）"},
									LineEdit{AssignTo: &btSeedRatioEdit, Text: strconv.Itoa(current.BTSeedRatioLimitPercent)},
									Label{Text: "做种时间（分钟）"},
									LineEdit{AssignTo: &btSeedTimeEdit, Text: strconv.Itoa(current.BTSeedTimeLimitMinutes)},
								},
							},
							Composite{
								Layout: Grid{Columns: 3},
								Children: []Widget{
									CheckBox{AssignTo: &btSequentialCheck, Text: "顺序下载", Checked: current.BTSequentialDownload},
									CheckBox{AssignTo: &btSaveMagnetCheck, Text: "保存磁力链接 .torrent 文件", Checked: current.BTSaveMagnetTorrentFile},
									CheckBox{AssignTo: &btEnableDHTCheck, Text: "DHT", Checked: current.BTEnableDHT},
									CheckBox{AssignTo: &btEnableLSDCheck, Text: "LSD", Checked: current.BTEnableLSD},
									CheckBox{AssignTo: &btEnableUPnPCheck, Text: "UPnP", Checked: current.BTEnableUPnP},
									CheckBox{AssignTo: &btEnableNATPMPCheck, Text: "NAT-PMP", Checked: current.BTEnableNATPMP},
								},
							},
							Label{Text: "附加 Tracker（每行一个）"},
							TextEdit{
								AssignTo: &btTrackersEdit,
								Text:     current.BTTrackersText,
								VScroll:  true,
								MinSize:  Size{Width: 0, Height: 60},
							},
						},
					},
					{
						Title:  "流媒体",
						Layout: VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
						Children: []Widget{
							Label{Text: "M3U8 / DASH"},
							Composite{
								Layout: Grid{Columns: 3},
								Children: []Widget{
									Label{Text: "FFmpeg"},
									LineEdit{
										AssignTo:   &ffmpegInstallDirEdit,
										Text:       current.FFmpegInstallDir,
										ColumnSpan: 1,
									},
									PushButton{
										Text: "浏览…",
										OnClicked: func() {
											dialog := new(walk.FileDialog)
											dialog.Title = "选择 FFmpeg 安装目录"
											if ok, err := dialog.ShowBrowseFolder(owner); err == nil && ok {
												ffmpegInstallDirEdit.SetText(dialog.FilePath)
											}
										},
									},
									Label{Text: "N_m3u8DL-RE"},
									LineEdit{
										AssignTo:   &m3u8InstallDirEdit,
										Text:       current.M3U8InstallDir,
										ColumnSpan: 1,
									},
									PushButton{
										Text: "浏览…",
										OnClicked: func() {
											dialog := new(walk.FileDialog)
											dialog.Title = "选择 N_m3u8DL-RE 安装目录"
											if ok, err := dialog.ShowBrowseFolder(owner); err == nil && ok {
												m3u8InstallDirEdit.SetText(dialog.FilePath)
											}
										},
									},
									Label{Text: "输出格式"},
									LineEdit{AssignTo: &m3u8OutputFormatEdit, Text: current.M3U8OutputFormat},
									Label{Text: "mp4 / mkv"},
									Label{Text: "线程数"},
									LineEdit{AssignTo: &m3u8ThreadEdit, Text: strconv.Itoa(current.M3U8ThreadCount)},
									Label{Text: "1 - 64"},
									Label{Text: "重试次数"},
									LineEdit{AssignTo: &m3u8RetryEdit, Text: strconv.Itoa(current.M3U8RetryCount)},
									Label{Text: "0+"},
									Label{Text: "请求超时"},
									LineEdit{AssignTo: &m3u8TimeoutEdit, Text: strconv.Itoa(current.M3U8RequestTimeoutSec)},
									Label{Text: "秒"},
									Label{Text: "字幕格式"},
									LineEdit{AssignTo: &m3u8SubtitleFormatEdit, Text: current.M3U8SubtitleFormat},
									Label{Text: "SRT / VTT"},
								},
							},
							Composite{
								Layout: Grid{Columns: 2},
								Children: []Widget{
									CheckBox{AssignTo: &m3u8ConcurrentCheck, Text: "并发下载音频、视频和字幕", Checked: current.M3U8ConcurrentDownload},
									CheckBox{AssignTo: &m3u8CheckSegmentsCheck, Text: "检查分片数量", Checked: current.M3U8CheckSegmentsCount},
									CheckBox{AssignTo: &m3u8DeleteAfterDoneCheck, Text: "完成后删除临时分片", Checked: current.M3U8DeleteAfterDone},
									CheckBox{AssignTo: &m3u8SelectAllCheck, Text: "选择全部音轨和字幕", Checked: current.M3U8SelectAllAudioSubtitle},
									CheckBox{AssignTo: &m3u8MP4DecryptCheck, Text: "MP4 实时解密", Checked: current.M3U8MP4RealTimeDecryption},
								},
							},
						},
					},
					{
						Title:  "请求",
						Layout: VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
						Children: []Widget{
							Label{Text: "请求标头"},
							TextEdit{
								AssignTo: &headersEdit,
								Text:     current.HeadersText,
								VScroll:  true,
								MinSize:  Size{Width: 0, Height: 110},
							},
							Label{Text: "Cookie"},
							TextEdit{
								AssignTo: &cookiesEdit,
								Text:     current.CookiesText,
								VScroll:  true,
								MinSize:  Size{Width: 0, Height: 90},
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &acceptButton,
						Text:     "保存",
						OnClicked: func() {
							parsed, err := settingsFromDialog(
								next,
								downloadDirEdit.Text(),
								proxyEdit.Text(),
								headersEdit.Text(),
								cookiesEdit.Text(),
								blockEdit.Text(),
								maxConcurrentEdit.Text(),
								retryEdit.Text(),
								speedLimitEdit.Text(),
								browserEnabledCheck.Checked(),
								browserTokenEdit.Text(),
								browserPortEdit.Text(),
								btSettingsDialogValues{
									listenPortText:            btListenPortEdit.Text(),
									metadataTimeoutText:       btMetadataTimeoutEdit.Text(),
									connectionsLimitText:      btConnectionsLimitEdit.Text(),
									downloadRateLimitKiBText:  btDownloadRateEdit.Text(),
									uploadRateLimitKiBText:    btUploadRateEdit.Text(),
									seedRatioLimitPercentText: btSeedRatioEdit.Text(),
									seedTimeLimitMinutesText:  btSeedTimeEdit.Text(),
									trackersText:              btTrackersEdit.Text(),
									sequentialDownload:        btSequentialCheck.Checked(),
									enableDHT:                 btEnableDHTCheck.Checked(),
									enableLSD:                 btEnableLSDCheck.Checked(),
									enableUPnP:                btEnableUPnPCheck.Checked(),
									enableNATPMP:              btEnableNATPMPCheck.Checked(),
									saveMagnetTorrentFile:     btSaveMagnetCheck.Checked(),
								},
								ffmpegInstallDirEdit.Text(),
								m3u8InstallDirEdit.Text(),
								m3u8OutputFormatEdit.Text(),
								m3u8ThreadEdit.Text(),
								m3u8RetryEdit.Text(),
								m3u8TimeoutEdit.Text(),
								m3u8SubtitleFormatEdit.Text(),
								m3u8ConcurrentCheck.Checked(),
								m3u8CheckSegmentsCheck.Checked(),
								m3u8DeleteAfterDoneCheck.Checked(),
								m3u8SelectAllCheck.Checked(),
								m3u8MP4DecryptCheck.Checked(),
							)
							if err != nil {
								walk.MsgBox(dlg, "设置", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							next = parsed
							next.ThemeMode = themeModeValue(themeModeBox.CurrentIndex())
							dlg.Accept()
						},
					},
					PushButton{
						AssignTo:  &cancelButton,
						Text:      "取消",
						OnClicked: func() { dlg.Cancel() },
					},
				},
			},
		},
	}

	if err := dialog.Create(owner); err != nil {
		return current, false, err
	}
	themeStyle, err := newWindowThemeStyle(
		dlg,
		current.ThemeMode,
		downloadDirEdit, proxyEdit, blockEdit, maxConcurrentEdit, retryEdit, speedLimitEdit,
		browserTokenEdit, browserPortEdit, ffmpegInstallDirEdit,
		btListenPortEdit, btMetadataTimeoutEdit, btConnectionsLimitEdit,
		btDownloadRateEdit, btUploadRateEdit, btSeedRatioEdit, btSeedTimeEdit, btTrackersEdit,
		m3u8InstallDirEdit, m3u8OutputFormatEdit, m3u8ThreadEdit, m3u8RetryEdit,
		m3u8TimeoutEdit, m3u8SubtitleFormatEdit, headersEdit, cookiesEdit,
	)
	if err != nil {
		return current, false, fmt.Errorf("设置窗口样式失败：%w", err)
	}
	defer themeStyle.Dispose()
	result := dlg.Run()
	return next, result == walk.DlgCmdOK, nil
}

func themeModeIndex(mode string) int {
	switch mode {
	case "light":
		return 1
	case "dark":
		return 2
	default:
		return 0
	}
}

func themeModeValue(index int) string {
	switch index {
	case 1:
		return "light"
	case 2:
		return "dark"
	default:
		return "system"
	}
}

func settingsFromDialog(
	current config.Settings,
	downloadDir, proxyURL, headersText, cookiesText string,
	blockText, maxText, retryText, speedText string,
	browserExtensionEnabled bool,
	browserPairToken, browserPortText string,
	bt btSettingsDialogValues,
	ffmpegInstallDir, m3u8InstallDir, m3u8OutputFormat, m3u8ThreadText, m3u8RetryText, m3u8TimeoutText, m3u8SubtitleFormat string,
	m3u8ConcurrentDownload, m3u8CheckSegmentsCount, m3u8DeleteAfterDone, m3u8SelectAllAudioSubtitle, m3u8MP4RealTimeDecryption bool,
) (config.Settings, error) {
	blockNum, err := strconv.Atoi(strings.TrimSpace(blockText))
	if err != nil || blockNum <= 0 {
		return current, fmt.Errorf("分块数必须为正整数")
	}
	maxConcurrent, err := strconv.Atoi(strings.TrimSpace(maxText))
	if err != nil || maxConcurrent <= 0 {
		return current, fmt.Errorf("最大并发任务数必须为正整数")
	}
	retryCount, err := strconv.Atoi(strings.TrimSpace(retryText))
	if err != nil || retryCount < 0 {
		return current, fmt.Errorf("重试次数必须为 0 或正整数")
	}
	speedLimitKiB, err := strconv.ParseInt(strings.TrimSpace(speedText), 10, 64)
	if err != nil || speedLimitKiB < 0 {
		return current, fmt.Errorf("限速必须为 0 或正数（KiB/s）")
	}
	browserBridgePort, err := strconv.Atoi(strings.TrimSpace(browserPortText))
	if err != nil || browserBridgePort <= 0 || browserBridgePort > 65535 {
		return current, fmt.Errorf("浏览器扩展端口必须在 1 到 65535 之间")
	}
	browserPairToken = strings.TrimSpace(browserPairToken)
	if browserPairToken == "" {
		browserPairToken = config.NewBrowserPairToken()
	}
	btListenPort, err := strconv.Atoi(strings.TrimSpace(bt.listenPortText))
	if err != nil || btListenPort < 0 || btListenPort > 65535 {
		return current, fmt.Errorf("BT 监听端口必须在 0 到 65535 之间")
	}
	btMetadataTimeoutSec, err := strconv.Atoi(strings.TrimSpace(bt.metadataTimeoutText))
	if err != nil || btMetadataTimeoutSec < 5 || btMetadataTimeoutSec > 300 {
		return current, fmt.Errorf("BT 元数据超时必须在 5 到 300 秒之间")
	}
	btConnectionsLimit, err := strconv.Atoi(strings.TrimSpace(bt.connectionsLimitText))
	if err != nil || btConnectionsLimit < 20 || btConnectionsLimit > 2000 {
		return current, fmt.Errorf("BT 连接数必须在 20 到 2000 之间")
	}
	btDownloadRateLimitKiB, err := strconv.ParseInt(strings.TrimSpace(bt.downloadRateLimitKiBText), 10, 64)
	if err != nil || btDownloadRateLimitKiB < 0 {
		return current, fmt.Errorf("BT 下载限速必须为 0 或正数（KiB/s）")
	}
	btUploadRateLimitKiB, err := strconv.ParseInt(strings.TrimSpace(bt.uploadRateLimitKiBText), 10, 64)
	if err != nil || btUploadRateLimitKiB < 0 {
		return current, fmt.Errorf("BT 上传限速必须为 0 或正数（KiB/s）")
	}
	btSeedRatioLimitPercent, err := strconv.Atoi(strings.TrimSpace(bt.seedRatioLimitPercentText))
	if err != nil || btSeedRatioLimitPercent < 0 || btSeedRatioLimitPercent > 10000 {
		return current, fmt.Errorf("BT 做种分享率必须在 0%% 到 10000%% 之间")
	}
	btSeedTimeLimitMinutes, err := strconv.Atoi(strings.TrimSpace(bt.seedTimeLimitMinutesText))
	if err != nil || btSeedTimeLimitMinutes < 0 || btSeedTimeLimitMinutes > 43200 {
		return current, fmt.Errorf("BT 做种时间必须在 0 到 43200 分钟之间")
	}
	m3u8OutputFormat = strings.ToLower(strings.TrimSpace(m3u8OutputFormat))
	if m3u8OutputFormat != "mp4" && m3u8OutputFormat != "mkv" {
		return current, fmt.Errorf("M3U8 输出格式必须为 mp4 或 mkv")
	}
	m3u8ThreadCount, err := strconv.Atoi(strings.TrimSpace(m3u8ThreadText))
	if err != nil || m3u8ThreadCount <= 0 || m3u8ThreadCount > 64 {
		return current, fmt.Errorf("M3U8 线程数必须在 1 到 64 之间")
	}
	m3u8RetryCount, err := strconv.Atoi(strings.TrimSpace(m3u8RetryText))
	if err != nil || m3u8RetryCount < 0 {
		return current, fmt.Errorf("M3U8 重试次数必须为 0 或正整数")
	}
	m3u8TimeoutSec, err := strconv.Atoi(strings.TrimSpace(m3u8TimeoutText))
	if err != nil || m3u8TimeoutSec <= 0 {
		return current, fmt.Errorf("M3U8 请求超时必须为正整数")
	}
	m3u8SubtitleFormat = strings.ToUpper(strings.TrimSpace(m3u8SubtitleFormat))
	if m3u8SubtitleFormat != "SRT" && m3u8SubtitleFormat != "VTT" {
		return current, fmt.Errorf("M3U8 字幕格式必须为 SRT 或 VTT")
	}
	headersText = strings.TrimSpace(headersText)
	if _, err := httpdownload.ParseHeaders(headersText); err != nil {
		return current, fmt.Errorf("请求标头格式错误：%w", err)
	}
	current.DownloadDir = strings.TrimSpace(downloadDir)
	current.ProxyURL = strings.TrimSpace(proxyURL)
	current.HeadersText = headersText
	current.CookiesText = httpdownload.NormalizeCookies(cookiesText)
	current.BlockNum = blockNum
	current.MaxConcurrent = maxConcurrent
	current.RetryCount = retryCount
	current.SpeedLimitKiB = speedLimitKiB
	current.BrowserExtensionEnabled = browserExtensionEnabled
	current.BrowserPairToken = browserPairToken
	current.BrowserBridgePort = browserBridgePort
	current.BTListenPort = btListenPort
	current.BTMetadataTimeoutSec = btMetadataTimeoutSec
	current.BTConnectionsLimit = btConnectionsLimit
	current.BTDownloadRateLimitKiB = btDownloadRateLimitKiB
	current.BTUploadRateLimitKiB = btUploadRateLimitKiB
	current.BTSequentialDownload = bt.sequentialDownload
	current.BTEnableDHT = bt.enableDHT
	current.BTEnableLSD = bt.enableLSD
	current.BTEnableUPnP = bt.enableUPnP
	current.BTEnableNATPMP = bt.enableNATPMP
	current.BTSeedRatioLimitPercent = btSeedRatioLimitPercent
	current.BTSeedTimeLimitMinutes = btSeedTimeLimitMinutes
	current.BTSaveMagnetTorrentFile = bt.saveMagnetTorrentFile
	current.BTTrackersText = strings.TrimSpace(bt.trackersText)
	current.FFmpegInstallDir = strings.TrimSpace(ffmpegInstallDir)
	current.M3U8InstallDir = strings.TrimSpace(m3u8InstallDir)
	current.M3U8OutputFormat = m3u8OutputFormat
	current.M3U8ThreadCount = m3u8ThreadCount
	current.M3U8RetryCount = m3u8RetryCount
	current.M3U8RequestTimeoutSec = m3u8TimeoutSec
	current.M3U8SubtitleFormat = m3u8SubtitleFormat
	current.M3U8ConcurrentDownload = m3u8ConcurrentDownload
	current.M3U8CheckSegmentsCount = m3u8CheckSegmentsCount
	current.M3U8DeleteAfterDone = m3u8DeleteAfterDone
	current.M3U8SelectAllAudioSubtitle = m3u8SelectAllAudioSubtitle
	current.M3U8MP4RealTimeDecryption = m3u8MP4RealTimeDecryption
	return current, nil
}
