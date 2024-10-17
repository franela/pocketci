package pocketci

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalDispatcher_Dispatch(t *testing.T) {
	ld := &LocalDispatcher{
		queued:  []*PocketciPipeline{},
		running: map[int]*PocketciPipeline{},
		done:    map[int]*PocketciPipeline{},
	}

	err := ld.Dispatch(context.Background(), []*Pipeline{
		{Name: "checks", Exec: "test & lint", Module: "ci"},
		{Name: "publish", Exec: "publish", Module: "ci", PipelineDeps: []string{"checks"}},
	})
	require.NoError(t, err)

	for i, p := range ld.queued {
		fmt.Printf("%d - %s - %+v - %s\n", i, p.Name, p.Parents, p.Call)
	}

	ctx := context.Background()
	p1 := ld.GetPipeline(ctx)
	p2 := ld.GetPipeline(ctx)
	fmt.Printf("%+v\n", p1)
	fmt.Printf("%+v\n", p2)
	fmt.Printf("%+v\n", ld.GetPipeline(context.Background()))
	ld.PipelineDone(ctx, p1.ID)
	fmt.Printf("%+v\n", ld.GetPipeline(context.Background()))
	ld.PipelineDone(ctx, p2.ID)
	fmt.Printf("%+v\n", ld.GetPipeline(context.Background()))
}
