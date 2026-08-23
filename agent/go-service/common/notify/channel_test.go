package notify

import (
	"testing"
)

func TestChannelRegistry(t *testing.T) {
	// 内置三个渠道均已注册
	wantNames := map[string]bool{"webhook": true, "bark": true, "serverchan": true}
	for _, name := range channelOrder {
		if !wantNames[name] {
			t.Errorf("unexpected channel in registry: %q", name)
		}
	}
	if len(channels) != len(wantNames) {
		t.Errorf("channel count = %d, want %d", len(channels), len(wantNames))
	}
	// 注册表与遍历顺序一致
	if len(channelOrder) != len(channels) {
		t.Errorf("channelOrder len = %d, channels len = %d", len(channelOrder), len(channels))
	}
	for _, name := range channelOrder {
		if channels[name] == nil {
			t.Errorf("channel %q registered in order but missing from map", name)
		}
	}
}

func TestRegisterChannelDuplicate(t *testing.T) {
	// 重复注册同名渠道被忽略，不 panic 不覆盖
	before := len(channels)
	RegisterChannel(webhookChannel{})
	if len(channels) != before {
		t.Errorf("duplicate registration should be ignored")
	}
}
