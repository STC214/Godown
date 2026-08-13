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
	next := current

	dialog := Dialog{
		AssignTo:      &dlg,
		Title:         "Settings",
		Size:          Size{Width: 820, Height: 900},
		MinSize:       Size{Width: 760, Height: 720},
		Layout:        VBox{Margins: Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}},
		DefaultButton: &acceptButton,
		CancelButton:  &cancelButton,
		Children: []Widget{
			ScrollView{
				HorizontalFixed: true,
				Layout:          VBox{MarginsZero: true},
				Children: []Widget{
					Label{Text: "Download"},
					Composite{
						Layout: Grid{Columns: 3},
						Children: []Widget{
							Label{Text: "Folder"},
							LineEdit{
								AssignTo:   &downloadDirEdit,
								Text:       current.DownloadDir,
								ColumnSpan: 1,
							},
							PushButton{
								Text: "Browse",
								OnClicked: func() {
									dialog := new(walk.FileDialog)
									dialog.Title = "Choose Download Folder"
									if ok, err := dialog.ShowBrowseFolder(owner); err == nil && ok {
										downloadDirEdit.SetText(dialog.FilePath)
									}
								},
							},
							Label{Text: "Proxy URL"},
							LineEdit{
								AssignTo:   &proxyEdit,
								Text:       current.ProxyURL,
								ColumnSpan: 2,
							},
						},
					},
					Label{Text: "Limits"},
					Composite{
						Layout: Grid{Columns: 2},
						Children: []Widget{
							Label{Text: "Blocks"},
							LineEdit{AssignTo: &blockEdit, Text: strconv.Itoa(current.BlockNum)},
							Label{Text: "Max Tasks"},
							LineEdit{AssignTo: &maxConcurrentEdit, Text: strconv.Itoa(current.MaxConcurrent)},
							Label{Text: "Retries"},
							LineEdit{AssignTo: &retryEdit, Text: strconv.Itoa(current.RetryCount)},
							Label{Text: "Speed KiB/s"},
							LineEdit{AssignTo: &speedLimitEdit, Text: strconv.FormatInt(current.SpeedLimitKiB, 10)},
						},
					},
					Label{Text: "Browser Extension"},
					Composite{
						Layout: Grid{Columns: 3},
						Children: []Widget{
							Label{Text: "Enabled"},
							CheckBox{
								AssignTo:   &browserEnabledCheck,
								Checked:    current.BrowserExtensionEnabled,
								ColumnSpan: 2,
							},
							Label{Text: "Token"},
							LineEdit{
								AssignTo:   &browserTokenEdit,
								Text:       current.BrowserPairToken,
								ReadOnly:   true,
								ColumnSpan: 1,
							},
							PushButton{
								Text: "Regenerate",
								OnClicked: func() {
									browserTokenEdit.SetText(config.NewBrowserPairToken())
								},
							},
							Label{Text: "Port"},
							LineEdit{AssignTo: &browserPortEdit, Text: strconv.Itoa(current.BrowserBridgePort)},
							Label{Text: "default 14370"},
						},
					},
					Label{Text: "BitTorrent"},
					Composite{
						Layout: Grid{Columns: 4},
						Children: []Widget{
							Label{Text: "Listen Port"},
							LineEdit{AssignTo: &btListenPortEdit, Text: strconv.Itoa(current.BTListenPort)},
							Label{Text: "Metadata Timeout (s)"},
							LineEdit{AssignTo: &btMetadataTimeoutEdit, Text: strconv.Itoa(current.BTMetadataTimeoutSec)},
							Label{Text: "Connections"},
							LineEdit{AssignTo: &btConnectionsLimitEdit, Text: strconv.Itoa(current.BTConnectionsLimit)},
							Label{Text: "Download KiB/s"},
							LineEdit{AssignTo: &btDownloadRateEdit, Text: strconv.FormatInt(current.BTDownloadRateLimitKiB, 10)},
							Label{Text: "Upload KiB/s"},
							LineEdit{AssignTo: &btUploadRateEdit, Text: strconv.FormatInt(current.BTUploadRateLimitKiB, 10)},
							Label{Text: "Seed Ratio (%)"},
							LineEdit{AssignTo: &btSeedRatioEdit, Text: strconv.Itoa(current.BTSeedRatioLimitPercent)},
							Label{Text: "Seed Time (min)"},
							LineEdit{AssignTo: &btSeedTimeEdit, Text: strconv.Itoa(current.BTSeedTimeLimitMinutes)},
						},
					},
					Composite{
						Layout: Grid{Columns: 3},
						Children: []Widget{
							CheckBox{AssignTo: &btSequentialCheck, Text: "Sequential", Checked: current.BTSequentialDownload},
							CheckBox{AssignTo: &btSaveMagnetCheck, Text: "Save magnet .torrent", Checked: current.BTSaveMagnetTorrentFile},
							CheckBox{AssignTo: &btEnableDHTCheck, Text: "DHT", Checked: current.BTEnableDHT},
							CheckBox{AssignTo: &btEnableLSDCheck, Text: "LSD", Checked: current.BTEnableLSD},
							CheckBox{AssignTo: &btEnableUPnPCheck, Text: "UPnP", Checked: current.BTEnableUPnP},
							CheckBox{AssignTo: &btEnableNATPMPCheck, Text: "NAT-PMP", Checked: current.BTEnableNATPMP},
						},
					},
					Label{Text: "Extra Trackers (one per line)"},
					TextEdit{
						AssignTo: &btTrackersEdit,
						Text:     current.BTTrackersText,
						VScroll:  true,
						MinSize:  Size{Width: 0, Height: 60},
					},
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
								Text: "Browse",
								OnClicked: func() {
									dialog := new(walk.FileDialog)
									dialog.Title = "Choose FFmpeg Install Folder"
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
								Text: "Browse",
								OnClicked: func() {
									dialog := new(walk.FileDialog)
									dialog.Title = "Choose N_m3u8DL-RE Install Folder"
									if ok, err := dialog.ShowBrowseFolder(owner); err == nil && ok {
										m3u8InstallDirEdit.SetText(dialog.FilePath)
									}
								},
							},
							Label{Text: "Output"},
							LineEdit{AssignTo: &m3u8OutputFormatEdit, Text: current.M3U8OutputFormat},
							Label{Text: "mp4 / mkv"},
							Label{Text: "Threads"},
							LineEdit{AssignTo: &m3u8ThreadEdit, Text: strconv.Itoa(current.M3U8ThreadCount)},
							Label{Text: "1 - 64"},
							Label{Text: "Retries"},
							LineEdit{AssignTo: &m3u8RetryEdit, Text: strconv.Itoa(current.M3U8RetryCount)},
							Label{Text: "0+"},
							Label{Text: "Timeout"},
							LineEdit{AssignTo: &m3u8TimeoutEdit, Text: strconv.Itoa(current.M3U8RequestTimeoutSec)},
							Label{Text: "seconds"},
							Label{Text: "Subtitle"},
							LineEdit{AssignTo: &m3u8SubtitleFormatEdit, Text: current.M3U8SubtitleFormat},
							Label{Text: "SRT / VTT"},
						},
					},
					Composite{
						Layout: Grid{Columns: 2},
						Children: []Widget{
							CheckBox{AssignTo: &m3u8ConcurrentCheck, Text: "Concurrent audio/video/subtitle", Checked: current.M3U8ConcurrentDownload},
							CheckBox{AssignTo: &m3u8CheckSegmentsCheck, Text: "Check segment count", Checked: current.M3U8CheckSegmentsCount},
							CheckBox{AssignTo: &m3u8DeleteAfterDoneCheck, Text: "Delete temp segments after done", Checked: current.M3U8DeleteAfterDone},
							CheckBox{AssignTo: &m3u8SelectAllCheck, Text: "Select all audio/subtitles", Checked: current.M3U8SelectAllAudioSubtitle},
							CheckBox{AssignTo: &m3u8MP4DecryptCheck, Text: "MP4 real-time decryption", Checked: current.M3U8MP4RealTimeDecryption},
						},
					},
					Label{Text: "Request Headers"},
					TextEdit{
						AssignTo: &headersEdit,
						Text:     current.HeadersText,
						VScroll:  true,
						MinSize:  Size{Width: 0, Height: 110},
					},
					Label{Text: "Cookies"},
					TextEdit{
						AssignTo: &cookiesEdit,
						Text:     current.CookiesText,
						VScroll:  true,
						MinSize:  Size{Width: 0, Height: 90},
					},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{
						AssignTo: &acceptButton,
						Text:     "Save",
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
								walk.MsgBox(dlg, "Settings", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							next = parsed
							dlg.Accept()
						},
					},
					PushButton{
						AssignTo:  &cancelButton,
						Text:      "Cancel",
						OnClicked: func() { dlg.Cancel() },
					},
				},
			},
		},
	}

	result, err := dialog.Run(owner)
	if err != nil {
		return current, false, err
	}
	return next, result == walk.DlgCmdOK, nil
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
		return current, fmt.Errorf("blocks must be positive")
	}
	maxConcurrent, err := strconv.Atoi(strings.TrimSpace(maxText))
	if err != nil || maxConcurrent <= 0 {
		return current, fmt.Errorf("max tasks must be positive")
	}
	retryCount, err := strconv.Atoi(strings.TrimSpace(retryText))
	if err != nil || retryCount < 0 {
		return current, fmt.Errorf("retries must be 0 or a positive number")
	}
	speedLimitKiB, err := strconv.ParseInt(strings.TrimSpace(speedText), 10, 64)
	if err != nil || speedLimitKiB < 0 {
		return current, fmt.Errorf("speed limit must be 0 or a positive KiB/s value")
	}
	browserBridgePort, err := strconv.Atoi(strings.TrimSpace(browserPortText))
	if err != nil || browserBridgePort <= 0 || browserBridgePort > 65535 {
		return current, fmt.Errorf("browser bridge port must be between 1 and 65535")
	}
	browserPairToken = strings.TrimSpace(browserPairToken)
	if browserPairToken == "" {
		browserPairToken = config.NewBrowserPairToken()
	}
	btListenPort, err := strconv.Atoi(strings.TrimSpace(bt.listenPortText))
	if err != nil || btListenPort < 0 || btListenPort > 65535 {
		return current, fmt.Errorf("bt listen port must be between 0 and 65535")
	}
	btMetadataTimeoutSec, err := strconv.Atoi(strings.TrimSpace(bt.metadataTimeoutText))
	if err != nil || btMetadataTimeoutSec < 5 || btMetadataTimeoutSec > 300 {
		return current, fmt.Errorf("bt metadata timeout must be between 5 and 300 seconds")
	}
	btConnectionsLimit, err := strconv.Atoi(strings.TrimSpace(bt.connectionsLimitText))
	if err != nil || btConnectionsLimit < 20 || btConnectionsLimit > 2000 {
		return current, fmt.Errorf("bt connections must be between 20 and 2000")
	}
	btDownloadRateLimitKiB, err := strconv.ParseInt(strings.TrimSpace(bt.downloadRateLimitKiBText), 10, 64)
	if err != nil || btDownloadRateLimitKiB < 0 {
		return current, fmt.Errorf("bt download limit must be 0 or a positive KiB/s value")
	}
	btUploadRateLimitKiB, err := strconv.ParseInt(strings.TrimSpace(bt.uploadRateLimitKiBText), 10, 64)
	if err != nil || btUploadRateLimitKiB < 0 {
		return current, fmt.Errorf("bt upload limit must be 0 or a positive KiB/s value")
	}
	btSeedRatioLimitPercent, err := strconv.Atoi(strings.TrimSpace(bt.seedRatioLimitPercentText))
	if err != nil || btSeedRatioLimitPercent < 0 || btSeedRatioLimitPercent > 10000 {
		return current, fmt.Errorf("bt seed ratio must be between 0 and 10000 percent")
	}
	btSeedTimeLimitMinutes, err := strconv.Atoi(strings.TrimSpace(bt.seedTimeLimitMinutesText))
	if err != nil || btSeedTimeLimitMinutes < 0 || btSeedTimeLimitMinutes > 43200 {
		return current, fmt.Errorf("bt seed time must be between 0 and 43200 minutes")
	}
	m3u8OutputFormat = strings.ToLower(strings.TrimSpace(m3u8OutputFormat))
	if m3u8OutputFormat != "mp4" && m3u8OutputFormat != "mkv" {
		return current, fmt.Errorf("m3u8 output must be mp4 or mkv")
	}
	m3u8ThreadCount, err := strconv.Atoi(strings.TrimSpace(m3u8ThreadText))
	if err != nil || m3u8ThreadCount <= 0 || m3u8ThreadCount > 64 {
		return current, fmt.Errorf("m3u8 threads must be between 1 and 64")
	}
	m3u8RetryCount, err := strconv.Atoi(strings.TrimSpace(m3u8RetryText))
	if err != nil || m3u8RetryCount < 0 {
		return current, fmt.Errorf("m3u8 retries must be 0 or a positive number")
	}
	m3u8TimeoutSec, err := strconv.Atoi(strings.TrimSpace(m3u8TimeoutText))
	if err != nil || m3u8TimeoutSec <= 0 {
		return current, fmt.Errorf("m3u8 timeout must be positive")
	}
	m3u8SubtitleFormat = strings.ToUpper(strings.TrimSpace(m3u8SubtitleFormat))
	if m3u8SubtitleFormat != "SRT" && m3u8SubtitleFormat != "VTT" {
		return current, fmt.Errorf("m3u8 subtitle format must be SRT or VTT")
	}
	headersText = strings.TrimSpace(headersText)
	if _, err := httpdownload.ParseHeaders(headersText); err != nil {
		return current, fmt.Errorf("headers invalid: %w", err)
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
