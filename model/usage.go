package model

// usageFold is the single usage and body ledger of one terminal reduction. Every
// observed response deposits its usage here, while only a response carrying valid
// content may retain the body slot.
//
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
type usageFold struct {
	usage Usage
	body  *Response
}

// merge adds one response's usage to the accumulated totals, including the
// generation timing durations.
func (f *usageFold) merge(resp *Response) {
	if resp == nil || resp.Usage == nil {
		return
	}
	f.usage.PromptTokens += resp.Usage.PromptTokens
	f.usage.CompletionTokens += resp.Usage.CompletionTokens
	f.usage.TotalTokens += resp.Usage.TotalTokens
	f.usage.PromptTokensDetails.CachedTokens += resp.Usage.PromptTokensDetails.CachedTokens
	f.usage.PromptTokensDetails.CacheCreationTokens += resp.Usage.PromptTokensDetails.CacheCreationTokens
	f.usage.PromptTokensDetails.CacheReadTokens += resp.Usage.PromptTokensDetails.CacheReadTokens
	f.usage.CompletionTokensDetails.ReasoningTokens += resp.Usage.CompletionTokensDetails.ReasoningTokens
	if resp.Usage.TimingInfo != nil {
		if f.usage.TimingInfo == nil {
			f.usage.TimingInfo = &TimingInfo{}
		}
		f.usage.TimingInfo.FirstTokenDuration += resp.Usage.TimingInfo.FirstTokenDuration
		f.usage.TimingInfo.ReasoningDuration += resp.Usage.TimingInfo.ReasoningDuration
	}
}

// retain keeps one response as the retained body. A response without valid
// content, such as a usage-only tail packet, cannot take the slot, so retained
// content survives every tail packet.
func (f *usageFold) retain(resp *Response) {
	if resp.IsValidContent() {
		f.body = resp
	}
}

// totals returns the accumulated usage with its timing record copied out.
func (f *usageFold) totals() Usage {
	total := f.usage
	if f.usage.TimingInfo != nil {
		timing := *f.usage.TimingInfo
		total.TimingInfo = &timing
	}
	return total
}

// retained returns the response held in the body slot, or nil when no response
// carried valid content.
func (f *usageFold) retained() *Response {
	return f.body
}
