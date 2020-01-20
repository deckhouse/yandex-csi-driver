package driver

import (
	"sync"
)

type RwMap struct {
	volumes map[string]interface{}
	mutex   sync.RWMutex
}

func NewRwMap() *RwMap {
	return &RwMap{
		volumes: make(map[string]interface{}),
	}
}

func (rwMap *RwMap) PutVolId(volID string) {
	rwMap.mutex.Lock()
	defer rwMap.mutex.Unlock()

	rwMap.volumes[volID] = nil
}

func (rwMap *RwMap) RemoveVolId(volID string) {
	rwMap.mutex.Lock()
	defer rwMap.mutex.Unlock()

	delete(rwMap.volumes, volID)
}

func (rwMap *RwMap) VolIdExists(volID string) bool {
	rwMap.mutex.RLock()
	defer rwMap.mutex.RUnlock()

	_, ok := rwMap.volumes[volID]

	return ok
}
