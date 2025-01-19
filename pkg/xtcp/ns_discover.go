package xtcp

import (
	"log"
	"os"
	"sync"
	"time"
)

func (x *XTCP) discoverAllNamespaces() (nsMap *sync.Map) {

	if x.debugLevel > 10 {
		log.Println("discoverALLNamespaces start")
	}

	var nsMaps []*sync.Map
	x.netNsDirs.Range(func(key, value interface{}) bool {
		nm := x.discoverNamespaces(key.(string))
		if x.debugLevel > 10 {
			log.Printf("discoverALLNamespaces x.discoverNamespaces(netNsDir):%s", key.(string))
		}
		nsMaps = append(nsMaps, nm)
		return true
	})

	// if x.debugLevel > 1000 {
	// 	for _, n := range nsMaps {
	// 		n.Range(func(key, value interface{}) bool {
	// 			log.Printf("DEBUG k:%v", key.(string))
	// 			return true
	// 		})
	// 	}
	// }

	if len(nsMaps) == 1 {
		nsMap = nsMaps[0]
		return
	}

	// if x.debugLevel > 1000 {
	// 	log.Printf("discoverALLNamespaces len(nsMaps):%d > 1", len(nsMaps))
	// }

	for i := 1; i < len(nsMaps); i++ {
		if x.debugLevel > 10 {
			log.Printf("discoverALLNamespaces merge i:%d", i)
		}
		nsMaps[0] = mergeMaps(nsMaps[0], nsMaps[i])
	}

	if x.debugLevel > 10 {
		log.Println("discoverALLNamespaces merge complete")
	}
	nsMap = nsMaps[0]

	// if x.debugLevel > 1000 {
	// 	var i int
	// 	x.netNsDirs.Range(func(key, value interface{}) bool {
	// 		log.Printf("discoverALLNamespaces i:%d key:%s", i, key.(string))
	// 		i++
	// 		return true
	// 	})
	// }

	return nsMap
}

func mergeMaps(map1, map2 *sync.Map) *sync.Map {
	// Create a new sync.Map to store the merged result
	mergedMap := &sync.Map{}

	// Copy all entries from map1 to mergedMap
	map1.Range(func(key, value interface{}) bool {
		mergedMap.Store(key, value)
		return true
	})

	// Copy all entries from map2 to mergedMap
	map2.Range(func(key, value interface{}) bool {
		// Optional: Overwrite existing keys or not
		mergedMap.Store(key, value)
		return true
	})

	return mergedMap
}

// discoverNamespaces traverse /run/netns/* and returns
// a sync.Map with the netns names as keys
func (x *XTCP) discoverNamespaces(netNsDir string) (nsMap *sync.Map) {

	startTime := time.Now()
	defer func() {
		x.pH.WithLabelValues("discoverNamespaces", "complete", "counter").Observe(time.Since(startTime).Seconds())
	}()

	x.pC.WithLabelValues("discoverNamespaces", "start", "counter").Inc()

	nsMap = &sync.Map{}

	files, err := os.ReadDir(netNsDir)
	if err != nil {
		if x.debugLevel > 10 {
			log.Printf("discoverNamespaces Error reading namespace directory: %v", err)
		}
		return
	}

	for i, file := range files {
		if file.IsDir() {
			continue
		}
		nsName := netNsDir + file.Name()
		nsMap.Store(nsName, nil)

		x.pC.WithLabelValues("discoverNamespaces", "add", "counter").Inc()
		if x.debugLevel > 10 {
			log.Printf("discoverNamespaces i:%d nsMap.Store: %s\n", i, nsName)
		}
	}

	return nsMap
}
