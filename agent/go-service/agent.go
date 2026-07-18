package main

import (
	"path/filepath"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/bytedance/sonic"
	"github.com/rs/zerolog/log"
)

// runAgent 启动 MaaFramework Agent Server 子进程主循环。
// 由 MaaPiCli/MXU 等 Client 通过 `agent/go-service <socket_id>` 拉起。
func runAgent(identifier string) {
	log.Info().
		Str("identifier", identifier).
		Msg("Starting agent server")

	// MAA DLL 位于工作目录下的 maafw 子目录
	libDir := filepath.Join(getCwd(), "maafw")
	log.Info().
		Str("libDir", libDir).
		Msg("Initializing MAA framework")
	if err := maa.Init(
		maa.WithLibDir(libDir),
		maa.WithJSONEncoder(sonic.Marshal),
		maa.WithJSONDecoder(sonic.Unmarshal),
	); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize MAA framework")
	}
	defer maa.Release()
	log.Info().
		Msg("MAA framework initialized")

	userPath := getCwd()
	if err := maa.ConfigInitOption(userPath, "{}"); err != nil {
		log.Warn().
			Str("userPath", userPath).
			Err(err).
			Msg("Failed to init toolkit config option")
	} else {
		log.Info().
			Str("userPath", userPath).
			Msg("Toolkit config option initialized")
	}

	registerAll()

	if err := maa.AgentServerStartUp(identifier); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to start agent server")
	}
	log.Info().
		Msg("Agent server started")

	maa.AgentServerJoin()

	maa.AgentServerShutDown()
	log.Info().
		Msg("Agent server shutdown")
}
