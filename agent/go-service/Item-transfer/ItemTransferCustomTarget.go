package itemTransfer

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"

	"fmt"
	"runtime"
	"strings"
	"bytes"
	"image"
	"image/png"
	"time"
	"io"
	"os"
	"os/exec"
	"encoding/base64"
	"net"
	"net/http"
	"path/filepath"
)

type ItemTransferCustomTarget struct{}

func openCacheImage(ctx *maa.Context,img image.Image) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		return
	}
	imgBase64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	htmlDir := "./resource/image/ItemTransfer/html"
	htmlPath := htmlDir + "/html.html"
	pngPath := htmlDir + "/png/自定义.png"

	abs, _ := filepath.Abs(htmlDir)
	log.Info().Str("path", abs).Msg("静态目录检查")

	content, err := os.ReadFile(htmlPath)
	if err != nil {
		log.Error().Err(err).Msg("读取html模板失败")
		return
	}
	html := string(content)
	html = strings.Replace(html, "{{.ImageData}}", imgBase64, 1)
	html = strings.Replace(html, "{{.language}}", "zh-cn", 1)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	stopSignal := make(chan struct{})
	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir(htmlDir))
	server := &http.Server{Handler: mux}

	mux.Handle("/data/", http.StripPrefix("/data/", fileServer))
	// http:127.0.0.xx/data/xxx => ./html/xxx

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
	})

	mux.HandleFunc("/close", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprint(w, "OK")
		close(stopSignal)
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
    file, _, _ := r.FormFile("image")
    defer file.Close()
    out, _ := os.Create(pngPath)
    defer out.Close()
    io.Copy(out, file)
    fmt.Fprint(w, "OK")
	})

	mux.HandleFunc("/screenshot", func(w http.ResponseWriter, r *http.Request) {
		returnImg := getCacheImage(ctx)
		if returnImg == nil {
			http.Error(w, "无法获取截图", http.StatusInternalServerError)
			return
    }
		w.Header().Set("Content-Type", "image/png")
    w.Header().Set("Access-Control-Allow-Origin", "*")
		err := png.Encode(w, returnImg)
		if err != nil {
			http.Error(w, "图片编码失败", http.StatusInternalServerError)
			return
    }
	})

	go server.Serve(listener)
	openURL(url)

	select {
	case <-stopSignal:
			log.Info().Msg("关闭裁剪页面...")
	case <-time.After(10 * time.Minute):
			log.Info().Msg("会话超时，自动关闭裁剪页面...")
	}
	server.Close()
}

// 跨系统兼容
func openURL(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin": // macos
		cmd = "open"
		args = []string{url}
	default: // linux
		cmd = "xdg-open"
		args = []string{url}
	}
	exec.Command(cmd, args...).Start()
}

func getCacheImage(ctx *maa.Context) image.Image {
	var ctrl = ctx.GetTasker().GetController()
	ctrl.PostScreencap().Wait()
	newImg, err := ctrl.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to capture image")
		return nil
	}
	if newImg == nil {
		log.Error().Msg("Failed to capture image")
		return nil
	}
	return newImg
}

func (*ItemTransferCustomTarget) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	openCacheImage(ctx,getCacheImage(ctx))
	return true
}