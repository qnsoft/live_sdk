package live_sdk

import (
	"context"
	"sync"
	"time"

	"github.com/logrusorgru/aurora"
	"github.com/qnsoft/live_utils"
)

type StreamCollection struct {
	sync.RWMutex
	m map[string]*Stream
}

func (sc *StreamCollection) GetStream(streamPath string) *Stream {
	sc.RLock()
	defer sc.RUnlock()
	if s, ok := sc.m[streamPath]; ok {
		return s
	}
	return nil
}
func (sc *StreamCollection) Delete(streamPath string) {
	sc.Lock()
	delete(sc.m, streamPath)
	sc.Unlock()
}

func (sc *StreamCollection) ToList() (r []*Stream) {
	sc.RLock()
	defer sc.RUnlock()
	for _, s := range sc.m {
		r = append(r, s)
	}
	return
}

func (sc *StreamCollection) Range(f func(*Stream)) {
	sc.RLock()
	defer sc.RUnlock()
	for _, s := range sc.m {
		f(s)
	}
}

func init() {
	Streams.m = make(map[string]*Stream)
}

// Streams 所有的流集合
var Streams StreamCollection

//FindStream 根据流路径查找流
func FindStream(streamPath string) *Stream {
	return Streams.GetStream(streamPath)
}

// Publish 直接发布
func Publish(streamPath, t string) *Stream {
	var stream = &Stream{
		StreamPath: streamPath,
		Type:       t,
	}
	if stream.Publish() {
		return stream
	}
	return nil
}

// Stream 流定义
type Stream struct {
	context.Context `json:"-"`
	StreamPath      string
	Type            string        //流类型，来自发布者
	StartTime       time.Time     //流的创建时间
	Subscribers     []*Subscriber // 订阅者
	VideoTracks     Tracks
	AudioTracks     Tracks
	DataTracks      Tracks
	AutoUnPublish   bool              //	当无人订阅时自动停止发布
	Transcoding     map[string]string //转码配置，key：目标编码，value：发布者提供的编码
	subscribeMutex  sync.Mutex
	timeout         *time.Timer //更新时间用来做超时处理
	OnClose         func()      `json:"-"`
	ExtraProp       interface{} //额外的属性，用于实现子类化，减少map的使用
}

// 增加结束时的回调，使用类似Js的猴子补丁
func (r *Stream) AddOnClose(onClose func()) {
	if originOnClose := r.OnClose; originOnClose == nil {
		r.OnClose = onClose
	} else {
		r.OnClose = func() {
			originOnClose()
			onClose()
		}
	}
}

func (r *Stream) Update() {
	if r.timeout != nil {
		r.timeout.Reset(config.PublishTimeout)
	}
}

func (r *Stream) Close() {
	Streams.Lock()
	//如果没有发布过，就不需要进行处理
	if r.timeout == nil {
		Streams.Unlock()
		return
	}
	delete(Streams.m, r.StreamPath)
	r.timeout.Stop()
	r.timeout = nil // 防止重复调用Close
	Streams.Unlock()
	r.VideoTracks.Dispose()
	r.AudioTracks.Dispose()
	r.DataTracks.Dispose()
	live_utils.Print(aurora.Yellow("Stream destoryed :"), aurora.BrightCyan(r.StreamPath))
	TriggerHook(HOOK_STREAMCLOSE, r)
	r.OnClose()
}

// Publish 发布者进行发布操作
func (r *Stream) Publish() bool {
	Streams.Lock()
	defer Streams.Unlock()
	if _, ok := Streams.m[r.StreamPath]; ok {
		return false
	}
	var cancel context.CancelFunc
	r.Context, cancel = context.WithCancel(Ctx)
	r.VideoTracks.Init(r.Context)
	r.AudioTracks.Init(r.Context)
	r.DataTracks.Init(r.Context)
	r.AddOnClose(cancel)
	r.StartTime = time.Now()
	Streams.m[r.StreamPath] = r
	live_utils.Print(aurora.Green("Stream publish:"), aurora.BrightCyan(r.StreamPath))
	r.timeout = time.AfterFunc(config.PublishTimeout, func() {
		live_utils.Print(aurora.Yellow("Stream timeout:"), aurora.BrightCyan(r.StreamPath))
		r.Close()
	})
	//触发钩子
	TriggerHook(HOOK_PUBLISH, r)
	return true
}

func (r *Stream) WaitDataTrack(names ...string) *DataTrack {
	if !config.EnableVideo {
		return nil
	}
	if track := r.DataTracks.WaitTrack(names...); track != nil {
		return track.(*DataTrack)
	}
	return nil
}

func (r *Stream) WaitVideoTrack(names ...string) *VideoTrack {
	if !config.EnableVideo {
		return nil
	}
	if track := r.VideoTracks.WaitTrack(names...); track != nil {
		return track.(*VideoTrack)
	}
	return nil
}

// TODO: 触发转码逻辑
func (r *Stream) WaitAudioTrack(names ...string) *AudioTrack {
	if !config.EnableAudio {
		return nil
	}
	if track := r.AudioTracks.WaitTrack(names...); track != nil {
		return track.(*AudioTrack)
	}
	return nil
}

//Subscribe 订阅流
func (r *Stream) Subscribe(s *Subscriber) {
	if s.Stream = r; r.Err() == nil {
		s.SubscribeTime = time.Now()
		live_utils.Print(aurora.Sprintf(aurora.Yellow("subscribe :%s %s,to Stream %s"), aurora.Blue(s.Type), aurora.Cyan(s.ID), aurora.BrightCyan(r.StreamPath)))
		s.Context, s.cancel = context.WithCancel(r)
		r.subscribeMutex.Lock()
		r.Subscribers = append(r.Subscribers, s)
		TriggerHook(HOOK_SUBSCRIBE, s, len(r.Subscribers))
		r.subscribeMutex.Unlock()
		live_utils.Print(aurora.Sprintf(aurora.Yellow("%s subscriber %s added remains:%d"), aurora.BrightCyan(r.StreamPath), aurora.Cyan(s.ID), aurora.Blue(len(r.Subscribers))))
	}
}

//UnSubscribe 取消订阅流
func (r *Stream) UnSubscribe(s *Subscriber) {
	if r.Err() == nil {
		var deleted bool
		r.subscribeMutex.Lock()
		r.Subscribers, deleted = DeleteSliceItem_Subscriber(r.Subscribers, s)
		if deleted {
			live_utils.Print(aurora.Sprintf(aurora.Yellow("%s subscriber %s removed remains:%d"), aurora.BrightCyan(r.StreamPath), aurora.Cyan(s.ID), aurora.Blue(len(r.Subscribers))))
			l := len(r.Subscribers)
			TriggerHook(HOOK_UNSUBSCRIBE, s, l)
			if l == 0 && r.AutoUnPublish {
				r.Close()
			}
		}
		r.subscribeMutex.Unlock()
	}
}
func DeleteSliceItem_Subscriber(slice []*Subscriber, item *Subscriber) ([]*Subscriber, bool) {
	for i, val := range slice {
		if val == item {
			return append(slice[:i], slice[i+1:]...), true
		}
	}
	return slice, false
}
