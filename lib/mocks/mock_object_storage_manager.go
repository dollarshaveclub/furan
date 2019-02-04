// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/dollarshaveclub/furan/lib/s3 (interfaces: ObjectStorageManager)

// Package mocks is a generated GoMock package.
package mocks

import (
	s3 "github.com/dollarshaveclub/furan/lib/s3"
	gomock "github.com/golang/mock/gomock"
	io "io"
	reflect "reflect"
)

// MockObjectStorageManager is a mock of ObjectStorageManager interface
type MockObjectStorageManager struct {
	ctrl     *gomock.Controller
	recorder *MockObjectStorageManagerMockRecorder
}

// MockObjectStorageManagerMockRecorder is the mock recorder for MockObjectStorageManager
type MockObjectStorageManagerMockRecorder struct {
	mock *MockObjectStorageManager
}

// NewMockObjectStorageManager creates a new mock instance
func NewMockObjectStorageManager(ctrl *gomock.Controller) *MockObjectStorageManager {
	mock := &MockObjectStorageManager{ctrl: ctrl}
	mock.recorder = &MockObjectStorageManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockObjectStorageManager) EXPECT() *MockObjectStorageManagerMockRecorder {
	return m.recorder
}

// Exists mocks base method
func (m *MockObjectStorageManager) Exists(arg0 s3.ImageDescription, arg1 interface{}) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Exists", arg0, arg1)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Exists indicates an expected call of Exists
func (mr *MockObjectStorageManagerMockRecorder) Exists(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Exists", reflect.TypeOf((*MockObjectStorageManager)(nil).Exists), arg0, arg1)
}

// Pull mocks base method
func (m *MockObjectStorageManager) Pull(arg0 s3.ImageDescription, arg1 io.WriterAt, arg2 interface{}) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Pull", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// Pull indicates an expected call of Pull
func (mr *MockObjectStorageManagerMockRecorder) Pull(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Pull", reflect.TypeOf((*MockObjectStorageManager)(nil).Pull), arg0, arg1, arg2)
}

// Push mocks base method
func (m *MockObjectStorageManager) Push(arg0 s3.ImageDescription, arg1 io.Reader, arg2 interface{}) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Push", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// Push indicates an expected call of Push
func (mr *MockObjectStorageManagerMockRecorder) Push(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Push", reflect.TypeOf((*MockObjectStorageManager)(nil).Push), arg0, arg1, arg2)
}

// Size mocks base method
func (m *MockObjectStorageManager) Size(arg0 s3.ImageDescription, arg1 interface{}) (int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Size", arg0, arg1)
	ret0, _ := ret[0].(int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Size indicates an expected call of Size
func (mr *MockObjectStorageManagerMockRecorder) Size(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Size", reflect.TypeOf((*MockObjectStorageManager)(nil).Size), arg0, arg1)
}

// WriteFile mocks base method
func (m *MockObjectStorageManager) WriteFile(arg0 string, arg1 s3.ImageDescription, arg2 string, arg3 io.Reader, arg4 interface{}) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WriteFile", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// WriteFile indicates an expected call of WriteFile
func (mr *MockObjectStorageManagerMockRecorder) WriteFile(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WriteFile", reflect.TypeOf((*MockObjectStorageManager)(nil).WriteFile), arg0, arg1, arg2, arg3, arg4)
}
