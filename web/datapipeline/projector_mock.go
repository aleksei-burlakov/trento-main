// Code generated by mockery v0.0.0-dev. DO NOT EDIT.

package datapipeline

import mock "github.com/stretchr/testify/mock"

// MockProjector is an autogenerated mock type for the Projector type
type MockProjector struct {
	mock.Mock
}

// Project provides a mock function with given fields: dataCollectedEvent
func (_m *MockProjector) Project(dataCollectedEvent *DataCollectedEvent) error {
	ret := _m.Called(dataCollectedEvent)

	var r0 error
	if rf, ok := ret.Get(0).(func(*DataCollectedEvent) error); ok {
		r0 = rf(dataCollectedEvent)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
