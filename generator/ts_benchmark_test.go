package generator

import (
	"testing"
)

func BenchmarkTSGeneration(b *testing.B) {
	fds := buildFileDescriptorSet(b)
	gen := newPlugin(b, fds, []string{"user.proto"})
	opts := &Options{Lang: "ts", Domain: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range gen.Files {
			if f.Generate {
				if err := generateTypeScript(gen, f, opts); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}
