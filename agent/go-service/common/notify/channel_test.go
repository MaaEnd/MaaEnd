package notify

import (
	"testing"
)

func TestChannelRegistry(t *testing.T) {
	// 内置渠道均已注册
	wantNames := map[string]bool{"webhook": true, "bark": true, "serverchan": true, "telegram": true, "discord": true, "wecom": true, "ntfy": true, "gotify": true, "dingtalk": true}
	for _, name := range channelOrder {
		if !wantNames[name] {
			t.Errorf("unexpected channel in registry: %q", name)
		}
	}
	if len(channelFactories) != len(wantNames) {
		t.Errorf("channel count = %d, want %d", len(channelFactories), len(wantNames))
	}
	// 注册表与遍历顺序一致
	if len(channelOrder) != len(channelFactories) {
		t.Errorf("channelOrder len = %d, channelFactories len = %d", len(channelOrder), len(channelFactories))
	}
	for _, name := range channelOrder {
		if channelFactories[name] == nil {
			t.Errorf("channel %q registered in order but missing from map", name)
		}
	}
}

func TestRegisterChannelDuplicate(t *testing.T) {
	// 重复注册同名渠道被忽略，不 panic 不覆盖
	before := len(channelFactories)
	RegisterChannel(webhookChannel{})
	if len(channelFactories) != before {
		t.Errorf("duplicate registration should be ignored")
	}
}
