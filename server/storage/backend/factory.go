// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package backend

import (
	"fmt"
	"sync"
)

// BackendType identifies a storage backend implementation.
type BackendType string

const (
	// BackendTypeBolt is the default BoltDB backend.
	BackendTypeBolt BackendType = "bolt"
	// BackendTypeMemory is an in-memory backend for testing.
	BackendTypeMemory BackendType = "memory"
)

// BackendFactory is a function that creates a Backend from configuration.
type BackendFactory func(cfg BackendConfig) (Backend, error)

var (
	factoryMu sync.RWMutex
	factories = make(map[BackendType]BackendFactory)
)

// Register registers a BackendFactory for a given BackendType.
// This function is typically called during init() by backend implementations.
// It is safe to call from multiple goroutines.
func Register(name BackendType, factory BackendFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	if factory == nil {
		panic("backend: Register factory is nil")
	}
	if _, dup := factories[name]; dup {
		panic("backend: Register called twice for factory " + string(name))
	}
	factories[name] = factory
}

// Create creates a Backend using the registered factory for the given type.
// If the type is empty, it defaults to BackendTypeBolt.
func Create(backendType BackendType, cfg BackendConfig) (Backend, error) {
	if backendType == "" {
		backendType = BackendTypeBolt
	}

	factoryMu.RLock()
	factory, ok := factories[backendType]
	factoryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("backend: unknown backend type %q", backendType)
	}

	return factory(cfg)
}

// Available returns a list of all registered backend types.
func Available() []BackendType {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	types := make([]BackendType, 0, len(factories))
	for t := range factories {
		types = append(types, t)
	}
	return types
}

// IsRegistered checks if a backend type is registered.
func IsRegistered(backendType BackendType) bool {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	_, ok := factories[backendType]
	return ok
}

func init() {
	// Register the default BoltDB backend
	Register(BackendTypeBolt, func(cfg BackendConfig) (Backend, error) {
		return newBackend(cfg), nil
	})
}
