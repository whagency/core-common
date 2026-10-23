package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whagency/core-common/grpc/pb/userv1"
	"github.com/whagency/core-common/services/user"
)

func TestDecodeUserByNumberData_Success(t *testing.T) {
	data := &userv1.GetUserByNumberResponse{
		UserId:               101,
		Number:               202,
		Status:               "active",
		VerificationLevel:    "basic",
		VerificationLevelMax: true,
	}

	result, err := decodeUserByNumberData(data)

	assert.NoError(t, err)
	assert.Equal(t, user.User{
		ID:                   101,
		Number:               202,
		VerificationLevel:    user.VerificationLevelBasic,
		VerificationLevelMax: true,
		Status:               user.StatusActive,
	}, result)
}

func TestDecodeUserByNumberData_InvalidStatus(t *testing.T) {
	data := &userv1.GetUserByNumberResponse{
		Status:            "unexpected_status",
		VerificationLevel: "basic",
	}

	_, err := decodeUserByNumberData(data)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status:unexpected_status")
}

func TestDecodeUserByNumberData_InvalidVerificationLevel(t *testing.T) {
	data := &userv1.GetUserByNumberResponse{
		Status:            "active",
		VerificationLevel: "weird_level",
	}

	_, err := decodeUserByNumberData(data)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verificationLevel:weird_level")
}
