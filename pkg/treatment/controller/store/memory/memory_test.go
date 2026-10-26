package memory_test

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/memory"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/storetest"
)

func TestMemoryConformance(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return memory.New() })
}
