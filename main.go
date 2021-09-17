package live_sdk

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"time" // colorable

	"github.com/qnsoft/live_sdk/util"
	"github.com/qnsoft/live_utils"

	"github.com/BurntSushi/toml"
	. "github.com/logrusorgru/aurora"
)

var Version = "3.2.2"

var (
	config = &struct {
		EnableAudio    bool
		EnableVideo    bool
		PublishTimeout time.Duration
		MaxRingSize    int
	}{true, true, 60, 256}
	// ConfigRaw 配置信息的原始数据
	ConfigRaw     []byte
	StartTime     time.Time                        //启动时间
	Plugins       = make(map[string]*PluginConfig) // Plugins 所有的插件配置
	HasTranscoder bool
)

//PluginConfig 插件配置定义
type PluginConfig struct {
	Name      string                       //插件名称
	Config    interface{}                  //插件配置
	Version   string                       //插件版本
	Dir       string                       //插件代码路径
	Run       func()                       //插件启动函数
	HotConfig map[string]func(interface{}) //热修改配置
}

// InstallPlugin 安装插件
func InstallPlugin(opt *PluginConfig) {
	Plugins[opt.Name] = opt
	_, pluginFilePath, _, _ := runtime.Caller(1)
	opt.Dir = filepath.Dir(pluginFilePath)
	if parts := strings.Split(opt.Dir, "@"); len(parts) > 1 {
		opt.Version = parts[len(parts)-1]
	}
	live_utils.Print(Green("install plugin"), BrightCyan(opt.Name), BrightBlue(opt.Version))
}

func init() {
	if parts := strings.Split(live_utils.CurrentDir(), "@"); len(parts) > 1 {
		Version = parts[len(parts)-1]
	}
}

// Run 启动Monibuca引擎
func Run(configFile string) (err error) {
	util.CreateShutdownScript()
	StartTime = time.Now()
	if ConfigRaw, err = ioutil.ReadFile(configFile); err != nil {
		live_utils.Print(Red("read config file error:"), err)
		return
	}
	live_utils.Print(BgGreen(Black("Ⓜ starting live_go ")), BrightBlue(Version))
	var cg map[string]interface{}
	if _, err = toml.Decode(string(ConfigRaw), &cg); err == nil {
		if cfg, ok := cg["LiveSdk"]; ok {
			b, _ := json.Marshal(cfg)
			if err = json.Unmarshal(b, config); err != nil {
				log.Println(err)
			}
			config.PublishTimeout *= time.Second
		}
		for name, config := range Plugins {
			if cfg, ok := cg[name]; ok {
				b, _ := json.Marshal(cfg)
				if err = json.Unmarshal(b, config.Config); err != nil {
					log.Println(err)
					continue
				}
			} else if config.Config != nil {
				continue
			}
			if config.Run != nil {
				go config.Run()
			}
		}
	} else {
		live_utils.Print(Red("decode config file error:"), err)
	}
	return
}
