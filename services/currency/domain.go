package currency

import (
	"time"

	"github.com/shopspring/decimal"
)

const (
	DefaultAssetScale       int32     = 8
	DefaultAccountAssetCode AssetCode = "USDT"
)

type AssetCode string

// NewAssetCode creates a new AssetCode from a string value.
func NewAssetCode(code string) AssetCode {
	return AssetCode(code)
}

// String returns the string representation of the AssetCode.
func (a AssetCode) String() string {
	return string(a)
}

type AssetType uint8

const (
	AssetTypeCrypto AssetType = iota + 1
	AssetTypeFiat
)

var _assetTypeToString = map[AssetType]string{
	AssetTypeCrypto: "crypto",
	AssetTypeFiat:   "fiat",
}

// NewAssetType creates a new AssetType from a uint8 value.
func NewAssetType(t uint8) AssetType {
	return AssetType(t)
}

// String returns the string representation of the AssetType.
func (a AssetType) String() string {
	if str, ok := _assetTypeToString[a]; ok {
		return str
	}
	return "unknown"
}

// Uint8 returns the uint8 representation of the AssetType.
func (a AssetType) Uint8() uint8 {
	return uint8(a)
}

type RateMap map[string]Rate

type Rate struct {
	CurrencyFrom          AssetCode
	CurrencyTo            AssetCode
	Rate                  decimal.Decimal
	ChangePercent         *decimal.Decimal
	UpdateIntervalSeconds int64
	UpdatedAt             time.Time
}

type AssetMap map[AssetCode]Asset

// Asset describes a supported currency.
type Asset struct {
	Code      AssetCode
	Type      AssetType
	Scale     int32
	USDPrice  decimal.Decimal
	UpdatedAt time.Time
}
