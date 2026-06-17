package errors

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

//go:embed locale.zh.toml
var zhToml []byte

// Localizer 根据 msgId 加载对应的中文文案。
type Localizer struct {
	messages map[string]string // msgId -> message
}

// NewLocalizer 从嵌入的 locale.zh.toml 创建 Localizer。
func NewLocalizer() (*Localizer, error) {
	l := &Localizer{messages: make(map[string]string)}
	if err := l.load(zhToml); err != nil {
		return nil, fmt.Errorf("load locale: %w", err)
	}
	return l, nil
}

func (l *Localizer) load(data []byte) error {
	var sections map[string]map[string]string
	if err := toml.Unmarshal(data, &sections); err != nil {
		return err
	}
	for section, entries := range sections {
		for key, value := range entries {
			l.messages[section+"."+key] = value
		}
	}
	return nil
}

// GetMessage 返回 msgId 对应的中文文案；不存在则返回 msgId 本身。
func (l *Localizer) GetMessage(msgId string) string {
	if l == nil || l.messages == nil {
		return msgId
	}
	if msg, ok := l.messages[msgId]; ok {
		return msg
	}
	return msgId
}

var (
	defaultLocalizer     *Localizer
	defaultLocalizerOnce sync.Once
)

func defaultLocalizerInstance() *Localizer {
	defaultLocalizerOnce.Do(func() {
		defaultLocalizer, _ = NewLocalizer()
	})
	return defaultLocalizer
}

// GetMessage 使用默认 Localizer 获取文案。
func GetMessage(msgId string) string {
	return defaultLocalizerInstance().GetMessage(msgId)
}
