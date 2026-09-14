package operation_setting

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	AmountBonus    map[int]float64 `json:"amount_bonus"`    // 充值金额对应的赠送比例，例如 100 元 10 表示达到 100 元额外赠送 10%

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
	AmountBonus:    map[int]float64{},
}

// MaxTopUpBonusPercent bounds the administrator-configured bonus multiplier
// before it reaches wallet quota arithmetic.
const MaxTopUpBonusPercent = 1000

// GetTopUpBonusPercent returns the highest configured bonus tier that applies
// to the recharge amount. Invalid entries are ignored defensively so malformed
// legacy configuration can never create an unsafe billing multiplier. Calls
// without an amount return zero so a missing amount can never enable a global
// bonus accidentally.
func GetTopUpBonusPercent(amounts ...float64) float64 {
	if len(amounts) == 0 {
		return 0
	}
	amount := amounts[0]
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return 0
	}

	var matchedThreshold int
	var matchedPercent float64
	for threshold, percent := range paymentSetting.AmountBonus {
		if threshold <= 0 || float64(threshold) > amount ||
			math.IsNaN(percent) || math.IsInf(percent, 0) ||
			percent <= 0 || percent > MaxTopUpBonusPercent {
			continue
		}
		if threshold > matchedThreshold {
			matchedThreshold = threshold
			matchedPercent = percent
		}
	}
	return matchedPercent
}

// GetMaxTopUpBonusPercent returns the largest valid configured percentage.
// It is used for conservative single-request quota bounds.
func GetMaxTopUpBonusPercent() float64 {
	var maxPercent float64
	for _, percent := range paymentSetting.AmountBonus {
		if percent > maxPercent && !math.IsNaN(percent) && !math.IsInf(percent, 0) && percent <= MaxTopUpBonusPercent {
			maxPercent = percent
		}
	}
	return maxPercent
}

// GetTopUpBonusRules returns a sanitized copy suitable for API responses.
func GetTopUpBonusRules() map[int]float64 {
	rules := make(map[int]float64)
	for threshold, percent := range paymentSetting.AmountBonus {
		if threshold > 0 && percent > 0 && percent <= MaxTopUpBonusPercent && !math.IsNaN(percent) && !math.IsInf(percent, 0) {
			rules[threshold] = percent
		}
	}
	return rules
}

// ValidateAmountBonusJSON validates the persisted amount-based bonus map.
// Keys are positive recharge amount thresholds and values are percentages.
func ValidateAmountBonusJSON(value string) error {
	raw := json.RawMessage(strings.TrimSpace(value))
	if common.GetJsonType(raw) != "object" {
		return fmt.Errorf("充值赠送配置必须是 JSON 对象")
	}

	var rawRules map[string]json.RawMessage
	if err := common.Unmarshal(raw, &rawRules); err != nil {
		return fmt.Errorf("解析充值赠送配置失败: %w", err)
	}
	for thresholdText, rawPercent := range rawRules {
		threshold, err := strconv.Atoi(thresholdText)
		if err != nil || threshold <= 0 {
			return fmt.Errorf("充值赠送门槛 %q 必须是正整数", thresholdText)
		}
		if common.GetJsonType(rawPercent) != "number" {
			return fmt.Errorf("充值赠送比例 %q 必须是数字", thresholdText)
		}
		var percent float64
		if err := common.Unmarshal(rawPercent, &percent); err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) || percent <= 0 || percent > MaxTopUpBonusPercent {
			return fmt.Errorf("充值赠送比例 %q 必须在 0 到 %d%% 之间", thresholdText, MaxTopUpBonusPercent)
		}
	}
	return nil
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}
