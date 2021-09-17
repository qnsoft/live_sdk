package live_sdk

import (
	"time"

	"github.com/qnsoft/live_utils/codec"
)

type AudioPack struct {
	AVPack
	Raw []byte
}
type AudioTrack struct {
	AVTrack
	SoundRate       int                             //2bit
	SoundSize       byte                            //1bit
	Channels        byte                            //1bit
	ExtraData       []byte                          `json:"-"` //rtmp协议需要先发这个帧
	PushByteStream  func(ts uint32, payload []byte) `json:"-"`
	PushRaw         func(ts uint32, payload []byte) `json:"-"`
	writeByteStream func()                          //使用函数写入，避免申请内存
	*AudioPack      `json:"-"`                      // 当前正在写入的音频对象

}

func (at *AudioTrack) pushByteStream(ts uint32, payload []byte) {
	switch at.CodecID = payload[0] >> 4; at.CodecID {
	case 10:
		if payload[1] != 0 {
			return
		} else {
			config1, config2 := payload[2], payload[3]
			//audioObjectType = (config1 & 0xF8) >> 3
			// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
			// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
			// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
			// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
			at.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
			at.Channels = ((config2 >> 3) & 0x0F) //声道
			//frameLengthFlag = (config2 >> 2) & 0x01
			//dependsOnCoreCoder = (config2 >> 1) & 0x01
			//extensionFlag = config2 & 0x01
			at.ExtraData = payload
			at.timebase = time.Duration(at.SoundRate)
			at.PushByteStream = func(ts uint32, payload []byte) {
				if len(payload) < 3 {
					return
				}
				at.setTS(ts)
				at.Raw = payload[2:]
				at.Payload = payload
				at.push()
			}
			at.Stream.AudioTracks.AddTrack("aac", at)
		}
	default:
		at.SoundRate = codec.SoundRate[(payload[0]&0x0c)>>2] // 采样率 0 = 5.5 kHz or 1 = 11 kHz or 2 = 22 kHz or 3 = 44 kHz
		at.SoundSize = (payload[0] & 0x02) >> 1              // 采样精度 0 = 8-bit samples or 1 = 16-bit samples
		at.Channels = payload[0]&0x01 + 1
		at.ExtraData = payload[:1]
		at.timebase = time.Duration(at.SoundRate)
		at.PushByteStream = func(ts uint32, payload []byte) {
			if len(payload) < 2 {
				return
			}
			at.setTS(ts)
			at.Raw = payload[1:]
			at.Payload = payload
			at.push()
		}
		switch at.CodecID {
		case 7:
			at.Stream.AudioTracks.AddTrack("pcma", at)
		case 8:
			at.Stream.AudioTracks.AddTrack("pcmu", at)
		}
		at.PushByteStream(ts, payload)
	}

}

func (at *AudioTrack) setCurrent() {
	at.AVTrack.setCurrent()
	at.AudioPack = at.Value.(*AudioPack)
}

func (at *AudioTrack) pushRaw(ts uint32, payload []byte) {
	switch at.CodecID {
	case 10:
		at.writeByteStream = func() {
			at.Reset()
			at.Buffer.Write([]byte{at.ExtraData[0], 1})
			at.Buffer.Write(at.Raw)
			at.Bytes2Payload()
		}
	default:
		at.writeByteStream = func() {
			at.Reset()
			at.WriteByte(at.ExtraData[0])
			at.Buffer.Write(at.Raw)
			at.Bytes2Payload()
		}
	}
	at.PushRaw = func(ts uint32, payload []byte) {
		at.setTS(ts)
		at.Raw = payload
		at.push()
	}
	at.PushRaw(ts, payload)
}

// Push 来自发布者推送的音频
func (at *AudioTrack) push() {
	if at.Stream != nil {
		at.Stream.Update()
	}
	if at.writeByteStream != nil {
		at.writeByteStream()
	}
	at.addBytes(len(at.Raw))
	at.GetBPS()
	if at.Timestamp.Sub(at.ts) > time.Second {
		at.resetBPS()
	}
	at.Step()
	at.setCurrent()
}

func (s *Stream) NewAudioTrack(codec byte) (at *AudioTrack) {
	at = &AudioTrack{}
	at.timebase = 8000
	at.CodecID = codec
	at.PushByteStream = at.pushByteStream
	at.PushRaw = at.pushRaw
	at.Stream = s
	at.Init(s.Context, 256)
	at.poll = time.Millisecond * 10
	at.Do(func(v interface{}) {
		v.(*AVItem).Value = new(AudioPack)
	})
	at.setCurrent()
	switch codec {
	case 10:
		s.AudioTracks.AddTrack("aac", at)
	case 7:
		s.AudioTracks.AddTrack("pcma", at)
	case 8:
		s.AudioTracks.AddTrack("pcmu", at)
	}
	return
}
func (at *AudioTrack) SetASC(asc []byte) {
	at.ExtraData = append([]byte{0xAF, 0}, asc...)
	config1 := asc[0]
	config2 := asc[1]
	at.CodecID = 10
	//audioObjectType = (config1 & 0xF8) >> 3
	// 1 AAC MAIN 	ISO/IEC 14496-3 subpart 4
	// 2 AAC LC 	ISO/IEC 14496-3 subpart 4
	// 3 AAC SSR 	ISO/IEC 14496-3 subpart 4
	// 4 AAC LTP 	ISO/IEC 14496-3 subpart 4
	at.SoundRate = codec.SamplingFrequencies[((config1&0x7)<<1)|(config2>>7)]
	at.Channels = (config2 >> 3) & 0x0F //声道
	//frameLengthFlag = (config2 >> 2) & 0x01
	//dependsOnCoreCoder = (config2 >> 1) & 0x01
	//extensionFlag = config2 & 0x01
	at.timebase = time.Duration(at.SoundRate)
	at.Stream.AudioTracks.AddTrack("aac", at)
}

func (at *AudioTrack) Play(onAudio func(uint32, *AudioPack), exit1, exit2 <-chan struct{}) {
	ar := at.Clone()
	item, ap := ar.Read()
	for startTimestamp := item.Timestamp; ; item, ap = ar.Read() {
		select {
		case <-exit1:
			return
		case <-exit2:
			return
		default:
			onAudio(uint32(item.Timestamp.Sub(startTimestamp).Milliseconds()), ap.(*AudioPack))
			ar.MoveNext()
		}
	}
}
