package audio

import (
	"bytes"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/wav"
)

var (
	speakerOnce      sync.Once
	speakerReady     bool
	speakerFormat    beep.Format
	backgroundVolume *effects.Volume
	backgroundMutex  sync.Mutex
	quiet            bool
	verbose          bool
	logFunc          func(string, ...interface{})
)

// Init configures the audio package
func Init(quietMode, verboseMode bool, logger func(string, ...interface{})) {
	quiet = quietMode
	verbose = verboseMode
	logFunc = logger
}

func log(format string, args ...interface{}) {
	if logFunc != nil && verbose {
		logFunc(format, args...)
	}
}

func ensureSpeakerInitialized(format beep.Format) {
	speakerOnce.Do(func() {
		log("Setting up audio...")
		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		speakerFormat = format
		speakerReady = true
	})
}

// DecodeSound decodes WAV sound data into a streamer
func DecodeSound(soundData []byte) (beep.StreamSeekCloser, beep.Format, error) {
	if len(soundData) == 0 {
		log("Couldn't play sound (no data)")
		return nil, beep.Format{}, nil
	}

	streamer, format, err := wav.Decode(bytes.NewReader(soundData))
	if err != nil {
		log("Sound file couldn't be decoded: %v", err)
		return nil, beep.Format{}, err
	}

	return streamer, format, nil
}

// Play plays a sound synchronously (blocks until complete)
func Play(soundData []byte) {
	if quiet {
		return
	}

	streamer, format, err := DecodeSound(soundData)
	if err != nil || streamer == nil {
		return
	}
	defer streamer.Close()

	ensureSpeakerInitialized(format)

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))

	log("Playing sound...")
	<-done
	log("Sound finished")
}

// StopAll stops all currently playing sounds
func StopAll() {
	if !speakerReady {
		return
	}
	speaker.Clear()
}

// PlayAsync plays a sound asynchronously at the specified volume (dB)
func PlayAsync(soundData []byte, volumeDB float64) {
	PlayAsyncLoop(soundData, volumeDB, false)
}

// PlayAsyncLoop plays a sound asynchronously, optionally looping
func PlayAsyncLoop(soundData []byte, volumeDB float64, loop bool) {
	if quiet {
		return
	}

	streamer, format, err := DecodeSound(soundData)
	if err != nil || streamer == nil {
		return
	}

	ensureSpeakerInitialized(format)

	var finalStreamer beep.Streamer = streamer
	if loop {
		finalStreamer = beep.Loop(-1, streamer)
	}

	backgroundMutex.Lock()
	backgroundVolume = &effects.Volume{
		Streamer: finalStreamer,
		Base:     2,
		Volume:   volumeDB,
		Silent:   false,
	}
	backgroundMutex.Unlock()

	speaker.Play(beep.Seq(backgroundVolume, beep.Callback(func() {
		streamer.Close()
		backgroundMutex.Lock()
		backgroundVolume = nil
		backgroundMutex.Unlock()
	})))

	if loop {
		log("Started looping background sound...")
	} else {
		log("Started background sound...")
	}
}

// PlayWithDucking plays a foreground sound while ducking (lowering) any background audio
func PlayWithDucking(soundData []byte, foregroundVolumeDB float64) {
	if quiet {
		return
	}

	streamer, format, err := DecodeSound(soundData)
	if err != nil || streamer == nil {
		return
	}
	defer streamer.Close()

	ensureSpeakerInitialized(format)

	// Lower the background sound
	backgroundMutex.Lock()
	originalVolume := 0.0
	if backgroundVolume != nil {
		originalVolume = backgroundVolume.Volume
		go func() {
			steps := 10
			for i := 0; i < steps; i++ {
				backgroundMutex.Lock()
				if backgroundVolume != nil {
					backgroundVolume.Volume = originalVolume - (5.0 * float64(i) / float64(steps))
				}
				backgroundMutex.Unlock()
				time.Sleep(30 * time.Millisecond)
			}
		}()
	}
	backgroundMutex.Unlock()

	foregroundVolume := &effects.Volume{
		Streamer: streamer,
		Base:     2,
		Volume:   foregroundVolumeDB,
		Silent:   false,
	}

	done := make(chan bool)
	speaker.Play(beep.Seq(foregroundVolume, beep.Callback(func() {
		done <- true
	})))

	<-done

	// Fade background back up
	backgroundMutex.Lock()
	if backgroundVolume != nil {
		go func() {
			steps := 15
			for i := 0; i < steps; i++ {
				backgroundMutex.Lock()
				if backgroundVolume != nil {
					currentVol := originalVolume - 5.0 + (5.0 * float64(i) / float64(steps))
					backgroundVolume.Volume = currentVol
				}
				backgroundMutex.Unlock()
				time.Sleep(33 * time.Millisecond)
			}
			backgroundMutex.Lock()
			if backgroundVolume != nil {
				backgroundVolume.Volume = originalVolume
			}
			backgroundMutex.Unlock()
		}()
	}
	backgroundMutex.Unlock()

	log("Foreground sound finished, fading background back up")
}
