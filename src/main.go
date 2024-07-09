//go:build !js
// +build !js

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264reader"
)

const (
	h264FrameDuration = time.Second / 60
	WEBSOCKET_PORT    = 8081
)

func main() { //nolint

	// Create a video track to send video to
	videoTrack, videoTrackErr := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if videoTrackErr != nil {
		panic(videoTrackErr)
	}

	// Start Signaling server
	iceConnectedCtx, iceConnectedCtxCancel := context.WithCancel(context.Background())
	applicationContext, applicationContextCancel := context.WithCancel(context.Background())
	defer applicationContextCancel()

	fmt.Println("Starting web socket server...")
	http.HandleFunc("/signaling", func(writer http.ResponseWriter, request *http.Request) {
		signalingHandler(writer, request, videoTrack, iceConnectedCtxCancel, applicationContext)
	})
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", WEBSOCKET_PORT), nil))
	}()

	fmt.Printf("WebSocket server started on port %d\n", WEBSOCKET_PORT)
	go func() {
		fmt.Printf("Running ffmpeg with args: %v\n", os.Args[1:])
		dataPipe, err := RunCommand("ffmpeg", os.Args[1:]...)

		if err != nil {
			panic(err)
		}

		h264, h264Err := h264reader.NewReader(dataPipe)
		if h264Err != nil {
			panic(h264Err)
		}

		// Wait for connection established
		fmt.Println("Waiting for connection to be established")
		<-iceConnectedCtx.Done()
		fmt.Println("Waiting for connection to be established... done!")

		spsAndPpsCache := []byte{}

		for {
			nal, h264Err := h264.NextNAL()
			if h264Err == io.EOF {
				fmt.Printf("All video frames parsed and sent")
				os.Exit(0)
			}
			if h264Err != nil {
				panic(h264Err)
			}

			// fmt.Printf("Found next NAL: %v, Size: %d\n", nal.UnitType.String(), len(nal.Data))

			nal.Data = append([]byte{0x00, 0x00, 0x00, 0x01}, nal.Data...)

			// fmt.Printf("NAL Unit type: %s\n", nal.UnitType.String())
			if nal.UnitType == h264reader.NalUnitTypeSPS || nal.UnitType == h264reader.NalUnitTypePPS {
				spsAndPpsCache = append(spsAndPpsCache, nal.Data...)
				continue
			} else if nal.UnitType == h264reader.NalUnitTypeCodedSliceIdr {
				nal.Data = append(spsAndPpsCache, nal.Data...)
				spsAndPpsCache = []byte{}
			}

			if h264Err = videoTrack.WriteSample(media.Sample{Data: nal.Data, Duration: h264FrameDuration}); h264Err != nil {
				panic(h264Err)
			}
		}
	}()

	// Block forever
	select {}
}
