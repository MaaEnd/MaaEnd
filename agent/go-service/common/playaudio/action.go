package playaudio

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
	"github.com/rs/zerolog/log"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
)

const throttleInterval = 5 * time.Second

var (
	otoCtx     *oto.Context
	otoCtxOnce sync.Once
)

type playAudioParam struct {
	Base64Mp3           string  `json:"base64_mp3"`
	Volume              float64 `json:"volume,omitempty"`
	NotificationTitle   string  `json:"notification_title,omitempty"`
	NotificationMessage string  `json:"notification_message,omitempty"`
}

type PlayAudioAction struct {
	mu         sync.Mutex
	lastPlayed time.Time
}

var _ maa.CustomActionRunner = &PlayAudioAction{}

func (a *PlayAudioAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Warn().Msg("PlayAudio: nil arg")
		return false
	}

	var params playAudioParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("PlayAudio: parse custom_action_param failed")
		return false
	}

	if params.Base64Mp3 == "" {
		log.Warn().Msg("PlayAudio: empty base64_mp3")
		return false
	}

	a.mu.Lock()
	if time.Since(a.lastPlayed) < throttleInterval {
		a.mu.Unlock()
		return true
	}
	a.lastPlayed = time.Now()
	a.mu.Unlock()

	mp3Data, err := base64.StdEncoding.DecodeString(params.Base64Mp3)
	if err != nil {
		log.Error().Err(err).Msg("PlayAudio: base64 decode failed")
		return false
	}

	decoder, err := mp3.NewDecoder(bytes.NewReader(mp3Data))
	if err != nil {
		log.Error().Err(err).Msg("PlayAudio: mp3 new decoder failed")
		return false
	}

	var pcmBuf bytes.Buffer
	if _, err := io.Copy(&pcmBuf, decoder); err != nil {
		log.Error().Err(err).Msg("PlayAudio: mp3 decode failed")
		return false
	}
	pcmData := pcmBuf.Bytes()
	if len(pcmData) == 0 {
		log.Warn().Msg("PlayAudio: decoded PCM is empty")
		return false
	}

	sampleRate := decoder.SampleRate()

	otoCtxOnce.Do(func() {
		op := &oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		}
		var ready chan struct{}
		otoCtx, ready, err = oto.NewContext(op)
		if err != nil {
			log.Warn().Err(err).Msg("PlayAudio: oto context failed, no audio device")
			return
		}
		<-ready
	})
	if otoCtx == nil {
		return true
	}

	player := otoCtx.NewPlayer(bytes.NewReader(pcmData))
	if params.Volume > 0 {
		player.SetVolume(params.Volume)
	}
	player.Play()

	log.Info().
		Int("sample_rate", sampleRate).
		Int("pcm_bytes", len(pcmData)).
		Msg("PlayAudio: playing sound")

	if params.NotificationMessage != "" {
		msg := params.NotificationMessage
		if params.NotificationTitle != "" {
			msg = params.NotificationTitle + "\n" + msg
		}
		maafocus.Print(ctx, msg)
	}

	if params.NotificationTitle != "" && params.NotificationMessage != "" && runtime.GOOS == "windows" {
		go sendWindowsToast(params.NotificationTitle, params.NotificationMessage)
	}

	return true
}

func sendWindowsToast(title, message string) {
	ps := `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$tpl = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$tpl.GetElementsByTagName("text").Item(0).AppendChild($tpl.CreateTextNode("` + title + `")) | Out-Null
$tpl.GetElementsByTagName("text").Item(1).AppendChild($tpl.CreateTextNode("` + message + `")) | Out-Null
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("MaaEnd").Show([Windows.UI.Notifications.ToastNotification]::new($tpl))
`
	if err := exec.Command("powershell", "-NoProfile", "-Command", ps).Run(); err != nil {
		log.Debug().Err(err).Msg("PlayAudio: Windows toast notification failed")
	}
}
