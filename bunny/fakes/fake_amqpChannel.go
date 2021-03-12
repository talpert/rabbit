// Code generated by counterfeiter. DO NOT EDIT.
package fakes

import (
	"sync"

	"github.com/streadway/amqp"
)

type FakeAmqpChannel struct {
	CancelStub        func(string, bool) error
	cancelMutex       sync.RWMutex
	cancelArgsForCall []struct {
		arg1 string
		arg2 bool
	}
	cancelReturns struct {
		result1 error
	}
	cancelReturnsOnCall map[int]struct {
		result1 error
	}
	CloseStub        func() error
	closeMutex       sync.RWMutex
	closeArgsForCall []struct {
	}
	closeReturns struct {
		result1 error
	}
	closeReturnsOnCall map[int]struct {
		result1 error
	}
	ConsumeStub        func(string, string, bool, bool, bool, bool, amqp.Table) (<-chan amqp.Delivery, error)
	consumeMutex       sync.RWMutex
	consumeArgsForCall []struct {
		arg1 string
		arg2 string
		arg3 bool
		arg4 bool
		arg5 bool
		arg6 bool
		arg7 amqp.Table
	}
	consumeReturns struct {
		result1 <-chan amqp.Delivery
		result2 error
	}
	consumeReturnsOnCall map[int]struct {
		result1 <-chan amqp.Delivery
		result2 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeAmqpChannel) Cancel(arg1 string, arg2 bool) error {
	fake.cancelMutex.Lock()
	ret, specificReturn := fake.cancelReturnsOnCall[len(fake.cancelArgsForCall)]
	fake.cancelArgsForCall = append(fake.cancelArgsForCall, struct {
		arg1 string
		arg2 bool
	}{arg1, arg2})
	stub := fake.CancelStub
	fakeReturns := fake.cancelReturns
	fake.recordInvocation("Cancel", []interface{}{arg1, arg2})
	fake.cancelMutex.Unlock()
	if stub != nil {
		return stub(arg1, arg2)
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeAmqpChannel) CancelCallCount() int {
	fake.cancelMutex.RLock()
	defer fake.cancelMutex.RUnlock()
	return len(fake.cancelArgsForCall)
}

func (fake *FakeAmqpChannel) CancelCalls(stub func(string, bool) error) {
	fake.cancelMutex.Lock()
	defer fake.cancelMutex.Unlock()
	fake.CancelStub = stub
}

func (fake *FakeAmqpChannel) CancelArgsForCall(i int) (string, bool) {
	fake.cancelMutex.RLock()
	defer fake.cancelMutex.RUnlock()
	argsForCall := fake.cancelArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2
}

func (fake *FakeAmqpChannel) CancelReturns(result1 error) {
	fake.cancelMutex.Lock()
	defer fake.cancelMutex.Unlock()
	fake.CancelStub = nil
	fake.cancelReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeAmqpChannel) CancelReturnsOnCall(i int, result1 error) {
	fake.cancelMutex.Lock()
	defer fake.cancelMutex.Unlock()
	fake.CancelStub = nil
	if fake.cancelReturnsOnCall == nil {
		fake.cancelReturnsOnCall = make(map[int]struct {
			result1 error
		})
	}
	fake.cancelReturnsOnCall[i] = struct {
		result1 error
	}{result1}
}

func (fake *FakeAmqpChannel) Close() error {
	fake.closeMutex.Lock()
	ret, specificReturn := fake.closeReturnsOnCall[len(fake.closeArgsForCall)]
	fake.closeArgsForCall = append(fake.closeArgsForCall, struct {
	}{})
	stub := fake.CloseStub
	fakeReturns := fake.closeReturns
	fake.recordInvocation("Close", []interface{}{})
	fake.closeMutex.Unlock()
	if stub != nil {
		return stub()
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeAmqpChannel) CloseCallCount() int {
	fake.closeMutex.RLock()
	defer fake.closeMutex.RUnlock()
	return len(fake.closeArgsForCall)
}

func (fake *FakeAmqpChannel) CloseCalls(stub func() error) {
	fake.closeMutex.Lock()
	defer fake.closeMutex.Unlock()
	fake.CloseStub = stub
}

func (fake *FakeAmqpChannel) CloseReturns(result1 error) {
	fake.closeMutex.Lock()
	defer fake.closeMutex.Unlock()
	fake.CloseStub = nil
	fake.closeReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeAmqpChannel) CloseReturnsOnCall(i int, result1 error) {
	fake.closeMutex.Lock()
	defer fake.closeMutex.Unlock()
	fake.CloseStub = nil
	if fake.closeReturnsOnCall == nil {
		fake.closeReturnsOnCall = make(map[int]struct {
			result1 error
		})
	}
	fake.closeReturnsOnCall[i] = struct {
		result1 error
	}{result1}
}

func (fake *FakeAmqpChannel) Consume(arg1 string, arg2 string, arg3 bool, arg4 bool, arg5 bool, arg6 bool, arg7 amqp.Table) (<-chan amqp.Delivery, error) {
	fake.consumeMutex.Lock()
	ret, specificReturn := fake.consumeReturnsOnCall[len(fake.consumeArgsForCall)]
	fake.consumeArgsForCall = append(fake.consumeArgsForCall, struct {
		arg1 string
		arg2 string
		arg3 bool
		arg4 bool
		arg5 bool
		arg6 bool
		arg7 amqp.Table
	}{arg1, arg2, arg3, arg4, arg5, arg6, arg7})
	stub := fake.ConsumeStub
	fakeReturns := fake.consumeReturns
	fake.recordInvocation("Consume", []interface{}{arg1, arg2, arg3, arg4, arg5, arg6, arg7})
	fake.consumeMutex.Unlock()
	if stub != nil {
		return stub(arg1, arg2, arg3, arg4, arg5, arg6, arg7)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *FakeAmqpChannel) ConsumeCallCount() int {
	fake.consumeMutex.RLock()
	defer fake.consumeMutex.RUnlock()
	return len(fake.consumeArgsForCall)
}

func (fake *FakeAmqpChannel) ConsumeCalls(stub func(string, string, bool, bool, bool, bool, amqp.Table) (<-chan amqp.Delivery, error)) {
	fake.consumeMutex.Lock()
	defer fake.consumeMutex.Unlock()
	fake.ConsumeStub = stub
}

func (fake *FakeAmqpChannel) ConsumeArgsForCall(i int) (string, string, bool, bool, bool, bool, amqp.Table) {
	fake.consumeMutex.RLock()
	defer fake.consumeMutex.RUnlock()
	argsForCall := fake.consumeArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2, argsForCall.arg3, argsForCall.arg4, argsForCall.arg5, argsForCall.arg6, argsForCall.arg7
}

func (fake *FakeAmqpChannel) ConsumeReturns(result1 <-chan amqp.Delivery, result2 error) {
	fake.consumeMutex.Lock()
	defer fake.consumeMutex.Unlock()
	fake.ConsumeStub = nil
	fake.consumeReturns = struct {
		result1 <-chan amqp.Delivery
		result2 error
	}{result1, result2}
}

func (fake *FakeAmqpChannel) ConsumeReturnsOnCall(i int, result1 <-chan amqp.Delivery, result2 error) {
	fake.consumeMutex.Lock()
	defer fake.consumeMutex.Unlock()
	fake.ConsumeStub = nil
	if fake.consumeReturnsOnCall == nil {
		fake.consumeReturnsOnCall = make(map[int]struct {
			result1 <-chan amqp.Delivery
			result2 error
		})
	}
	fake.consumeReturnsOnCall[i] = struct {
		result1 <-chan amqp.Delivery
		result2 error
	}{result1, result2}
}

func (fake *FakeAmqpChannel) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.cancelMutex.RLock()
	defer fake.cancelMutex.RUnlock()
	fake.closeMutex.RLock()
	defer fake.closeMutex.RUnlock()
	fake.consumeMutex.RLock()
	defer fake.consumeMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeAmqpChannel) recordInvocation(key string, args []interface{}) {
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