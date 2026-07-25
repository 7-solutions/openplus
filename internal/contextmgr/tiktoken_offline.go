//go:build offline

// Offline BPE loader for tiktoken-go. Pulls in github.com/pkoukk/tiktoken-go-loader,
// which embeds the BPE tables as Go data so the binary needs no network on
// first use. The default build (no 'offline' tag) does NOT pull this loader in;
// TIKTOKEN_CACHE_DIR handles the network-then-cache path.
//
// Activation:
//
//	go build -tags offline ./cmd/openplus
//
// The loader is set once via tiktoken.SetBpeLoader in a package-level init
// here; subsequent tiktoken.GetEncoding calls reuse it. There is no
// SetBpeLoader.Reset — if a process wants both online and offline modes
// in one binary, that's a future change (probably two distinct encoding
// caches).

package contextmgr

import (
	"github.com/pkoukk/tiktoken-go-loader"

	"github.com/pkoukk/tiktoken-go"
)

func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}