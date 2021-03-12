// Code generated by counterfeiter. DO NOT EDIT.
package fakes

import (
	"sync"
)

type FakeLocker struct {
	LockStub        func()
	lockMutex       sync.RWMutex
	lockArgsForCall []struct {
	}
	UnlockStub        func()
	unlockMutex       sync.RWMutex
	unlockArgsForCall []struct {
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeLocker) Lock() {
	fake.lockMutex.Lock()
	fake.lockArgsForCall = append(fake.lockArgsForCall, struct {
	}{})
	stub := fake.LockStub
	fake.recordInvocation("Lock", []interface{}{})
	fake.lockMutex.Unlock()
	if stub != nil {
		fake.LockStub()
	}
}

func (fake *FakeLocker) LockCallCount() int {
	fake.lockMutex.RLock()
	defer fake.lockMutex.RUnlock()
	return len(fake.lockArgsForCall)
}

func (fake *FakeLocker) LockCalls(stub func()) {
	fake.lockMutex.Lock()
	defer fake.lockMutex.Unlock()
	fake.LockStub = stub
}

func (fake *FakeLocker) Unlock() {
	fake.unlockMutex.Lock()
	fake.unlockArgsForCall = append(fake.unlockArgsForCall, struct {
	}{})
	stub := fake.UnlockStub
	fake.recordInvocation("Unlock", []interface{}{})
	fake.unlockMutex.Unlock()
	if stub != nil {
		fake.UnlockStub()
	}
}

func (fake *FakeLocker) UnlockCallCount() int {
	fake.unlockMutex.RLock()
	defer fake.unlockMutex.RUnlock()
	return len(fake.unlockArgsForCall)
}

func (fake *FakeLocker) UnlockCalls(stub func()) {
	fake.unlockMutex.Lock()
	defer fake.unlockMutex.Unlock()
	fake.UnlockStub = stub
}

func (fake *FakeLocker) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.lockMutex.RLock()
	defer fake.lockMutex.RUnlock()
	fake.unlockMutex.RLock()
	defer fake.unlockMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeLocker) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ sync.Locker = new(FakeLocker)