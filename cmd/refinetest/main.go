package main

import (
	"context"
	"fmt"
	"time"

	"github.com/AdityaPainuli/whispr-go/internal/refine"
)

func main() {
	llm := refine.NewLlamaServer(
		"third_party/llama/llama-server",
		"models/qwen2.5-3b-instruct-q4_k_m.gguf",
		8181,
	)
	t0 := time.Now()
	if err := llm.Start(); err != nil {
		panic(err)
	}
	fmt.Printf("startup+warmup: %v\n", time.Since(t0))
	defer llm.Stop()

	cases := []string{
		"Hey, can you send me the report by tomorrow morning?",
		"Okay so the plan is first we fix the audio bug then we ship the app bundle then uh we start on the windows port i think that should take about two weeks maybe three if the hotkey stuff is as painful as it was on mac",
		"Um, so I think we should, uh, move the meeting to Thursday. You know, because Friday is packed.",
	}
	for _, text := range cases {
		// same timeout formula as session.refined()
		timeout := 600*time.Millisecond + time.Duration(len(text))*4*time.Millisecond
		if timeout > 2500*time.Millisecond {
			timeout = 2500 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		t := time.Now()
		out, err := llm.Refine(ctx, text)
		cancel()
		fmt.Printf("took %v (budget %v) err=%v\n  in : %s\n  out: %s\n", time.Since(t), timeout, err, text, out)
	}
}
