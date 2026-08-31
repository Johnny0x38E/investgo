package i18n

import "testing"

func TestLocalizePoolInstrumentErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "pool service unavailable",
			message: "Pool service is unavailable",
			want:    "证券池服务不可用",
		},
		{
			name:    "member already excluded",
			message: "Pool member is already excluded",
			want:    "该成员已被删除",
		},
		{
			name:    "member status validation",
			message: "Pool member status must be active or excluded",
			want:    "成员状态仅支持 active 或 excluded",
		},
		{
			name:    "missing instrument symbol",
			message: "invalid instrument symbol: is required",
			want:    "股票代码不能为空",
		},
		{
			name:    "a-share symbol length",
			message: "invalid instrument symbol: A-share and onshore ETF symbols must contain exactly 6 digits",
			want:    "股票代码无效: A股/境内ETF代码必须恰好为 6 位数字",
		},
		{
			name:    "hk symbol length",
			message: "invalid instrument symbol: Hong Kong symbols must contain 1 to 5 digits",
			want:    "股票代码无效: 港股代码必须为 1 至 5 位数字",
		},
		{
			name:    "us symbol charset",
			message: "invalid instrument symbol: US symbols may contain only letters, digits, and hyphens",
			want:    "股票代码无效: 美股代码只能包含字母、数字和连字符",
		},
		{
			name:    "user-added must be deleted",
			message: "invalid pool member operation: user-added members must be deleted, not excluded",
			want:    "无效的证券池操作: 用户添加的成员只能删除，无法排除",
		},
		{
			name:    "built-in exclusion must be restored",
			message: "invalid pool member operation: built-in exclusions must be restored",
			want:    "无效的证券池操作: 内置成员删除后只能通过恢复撤销",
		},
		{
			name:    "instrument outside pool market",
			message: "invalid pool member operation: instrument does not belong to pool market",
			want:    "无效的证券池操作: 标的不属于该证券池的市场",
		},
		{
			name:    "pool not found keeps id",
			message: "pool not found: pool-us-sp500",
			want:    "证券池不存在: pool-us-sp500",
		},
		{
			name:    "member not found keeps ids",
			message: "Pool member not found: pool-us-sp500/instrument-abc",
			want:    "证券池成员不存在: pool-us-sp500/instrument-abc",
		},
	}

	for _, entry := range tests {
		entry := entry
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			got := LocalizeErrorMessage("zh-CN", entry.message)
			if got != entry.want {
				t.Fatalf("LocalizeErrorMessage(%q) = %q, want %q", entry.message, got, entry.want)
			}
		})
	}
}

func TestLocalizePoolInstrumentErrorsKeepsEnglishForOtherLocales(t *testing.T) {
	t.Parallel()

	message := "invalid instrument symbol: is required"
	got := LocalizeErrorMessage("en-US", message)
	if got != message {
		t.Fatalf("LocalizeErrorMessage(en-US) = %q, want %q", got, message)
	}
}
