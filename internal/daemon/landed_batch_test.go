// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"testing"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

// batchingSource is a stubSource that also reads in batches, as source.Direct does.
type batchingSource struct {
	*stubSource
	asked [][]source.BatchRequest
}

func (b *batchingSource) FetchBatch(ctx context.Context, reqs []source.BatchRequest) ([]source.BatchResult, error) {
	b.asked = append(b.asked, reqs)
	return []source.BatchResult{{Parts: map[source.Part]source.BatchPart{source.Items: {Revision: "r1"}}}}, nil
}

func TestTheDaemonsFetcherBatchesWhenItsSourceDoes(t *testing.T) {
	d := &Daemon{}
	if _, ok := newLandingFetcher(&stubSource{}, d).(source.Batcher); ok {
		t.Fatal("the fetcher batches over a source that does not")
	}
	src := &batchingSource{stubSource: &stubSource{}}
	b, ok := newLandingFetcher(src, d).(source.Batcher)
	if !ok {
		t.Fatal("the fetcher hides its source's batched read from the cache")
	}
	reqs := []source.BatchRequest{{Project: registry.Project{Name: "a", Path: "/a"}, Parts: []source.Part{source.Items}}}
	res, err := b.FetchBatch(context.Background(), reqs)
	if err != nil || len(src.asked) != 1 || len(res) != 1 || res[0].Parts[source.Items].Revision != "r1" {
		t.Fatalf("batch: %+v, %v, asked %d", res, err, len(src.asked))
	}
}
