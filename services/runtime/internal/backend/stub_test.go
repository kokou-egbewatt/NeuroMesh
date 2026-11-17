package backend

import (
	"context"
	"testing"
)

func TestStubEchoesModelAndCountsWords(t *testing.T) {
	r, err := Stub{}.Complete(context.Background(), Request{Model: "llama3", Prompt: "hello there world"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Output != "stub response for model llama3" || r.Usage != (Usage{PromptTokens: 3, CompletionTokens: 5}) {
		t.Fatalf("got %+v", r)
	}
	var toks []string
	u, err := Stub{}.Stream(context.Background(), Request{Model: "llama3", Prompt: "hi"}, func(s string) error {
		toks = append(toks, s)
		return nil
	})
	if err != nil || len(toks) != 1 || toks[0] != "stub token for llama3" || u.CompletionTokens != 4 {
		t.Fatalf("stream %q %+v %v", toks, u, err)
	}
}
