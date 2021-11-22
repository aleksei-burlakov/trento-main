// Code generated by mockery v0.0.0-dev. DO NOT EDIT.

package services

import (
	uuid "github.com/google/uuid"
	mock "github.com/stretchr/testify/mock"
)

// MockSettingsService is an autogenerated mock type for the SettingsService type
type MockSettingsService struct {
	mock.Mock
}

// InitializeIdentifier provides a mock function with given fields:
func (_m *MockSettingsService) InitializeIdentifier() (uuid.UUID, error) {
	ret := _m.Called()

	var r0 uuid.UUID
	if rf, ok := ret.Get(0).(func() uuid.UUID); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(uuid.UUID)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
