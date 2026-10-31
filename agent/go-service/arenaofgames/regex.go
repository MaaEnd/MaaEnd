package arenaofgames

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	regexHandLimit = 5
	regexTileKinds = 15
)

type regexMatchState uint8

const (
	regexUnknown regexMatchState = iota
	regexComplete
	regexContinuing
	regexRejected
)

// regexMatcherCache 只保留当前配置，包括校验错误；重开一轮不会丢失已计算的状态。
// 与本包其余轮次状态一样，仅由 MaaFramework 的单线程任务回调访问，不保存框架句柄。
type regexMatcherCache struct {
	pattern   string
	unordered bool
	set       bool
	matcher   *regexMatcher
	err       error
}

// get 编译正则并从空手验证至少存在一种合法结果，避免无解配置反复放弃重开。
func (c *regexMatcherCache) get(pattern string, unordered bool) (*regexMatcher, error) {
	if c.set && c.pattern == pattern && c.unordered == unordered {
		return c.matcher, c.err
	}
	*c = regexMatcherCache{pattern: pattern, unordered: unordered, set: true}
	if strings.TrimSpace(pattern) == "" {
		c.err = fmt.Errorf("custom hand regex must not be blank")
		return nil, c.err
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		c.err = fmt.Errorf("compile custom hand regex: %w", err)
		return nil, c.err
	}
	// 最左最长匹配只要存在全串解就必定返回全串；不拼接 wrapper，以免改变 \Q 等语法。
	expression.Longest()
	matcher := &regexMatcher{expression: expression, unordered: unordered}
	if unordered {
		matcher.unorderedStates = make(map[uint64]regexMatchState)
	} else {
		// 每张牌以 1..15 编码，占 4 bit；最多 5 张，0 唯一表示空手。
		matcher.orderedStates = make([]regexMatchState, 1<<(4*regexHandLimit))
	}
	status, err := matcher.evaluate(handDetail{})
	if err != nil {
		c.err = err
		return nil, c.err
	}
	if status == "rejected" {
		c.err = fmt.Errorf("custom hand regex matches no legal hand of 1 to 5 cards")
		return nil, c.err
	}
	c.matcher = matcher
	return matcher, nil
}

type regexMatcher struct {
	expression      *regexp.Regexp
	unordered       bool
	orderedStates   []regexMatchState
	unorderedStates map[uint64]regexMatchState
}

type regexHand struct {
	cards    [2 * regexHandLimit]byte
	counts   [regexTileKinds]uint8
	size     int
	orderKey uint32
	countKey uint64
}

// evaluate 在当前手牌命中时早停，否则检查补至最多 5 张是否仍有解。
func (m *regexMatcher) evaluate(detail handDetail) (string, error) {
	if m == nil || m.expression == nil {
		return "", fmt.Errorf("custom hand regex is not initialized")
	}
	hand, err := encodeRegexHand(detail)
	if err != nil {
		return "", err
	}
	var state regexMatchState
	if m.unordered {
		state = m.unorderedStatus(&hand.counts, &hand.cards, hand.size, hand.countKey)
	} else {
		state = m.orderedStatus(&hand.cards, hand.size, hand.orderKey)
	}
	switch state {
	case regexComplete:
		return "complete", nil
	case regexContinuing:
		return "continuing", nil
	default:
		return "rejected", nil
	}
}

// encodeRegexHand 按槽位顺序编码，m/s/p 分别表示万/索/筒；只接受连续的完整手牌。
func encodeRegexHand(detail handDetail) (regexHand, error) {
	if detail.Count < 0 || detail.Count > regexHandLimit {
		return regexHand{}, fmt.Errorf("hand count must be between 0 and 5")
	}
	hand := regexHand{size: detail.Count}
	for slot, card := range detail.Slots {
		if slot >= detail.Count {
			if (card.Type != "" && card.Type != "back") || card.Point != 0 {
				return regexHand{}, fmt.Errorf("slot %d contains a card beyond the hand count", slot+1)
			}
			continue
		}
		var suit int
		switch card.Type {
		case "Character":
			suit = 0
		case "Bamboo":
			suit = 1
		case "Dot":
			suit = 2
		default:
			return regexHand{}, fmt.Errorf("slot %d has an invalid card suit", slot+1)
		}
		if card.Point < 1 || card.Point > 5 {
			return regexHand{}, fmt.Errorf("slot %d has an invalid card point", slot+1)
		}
		tile := suit*5 + card.Point - 1
		writeRegexTile(&hand.cards, slot, tile)
		hand.counts[tile]++
		hand.orderKey = hand.orderKey<<4 | uint32(tile+1)
		// 每种牌最多 5 张，3 bit 足以保存计数；相同多重集共享同一个键。
		hand.countKey += uint64(1) << (3 * tile)
	}
	return hand, nil
}

func writeRegexTile(cards *[2 * regexHandLimit]byte, slot, tile int) {
	cards[2*slot] = byte('1' + tile%5)
	cards[2*slot+1] = "msp"[tile/5]
}

func (m *regexMatcher) fullMatch(cards []byte) bool {
	span := m.expression.FindIndex(cards)
	return span != nil && span[0] == 0 && span[1] == len(cards)
}

// orderedStatus 只在末尾追加新牌；按序列缓存，不能用会丢失顺序的牌面计数去重。
func (m *regexMatcher) orderedStatus(cards *[2 * regexHandLimit]byte, size int, key uint32) regexMatchState {
	if cached := m.orderedStates[key]; cached != regexUnknown {
		return cached
	}
	state := regexRejected
	if size > 0 && m.fullMatch(cards[:2*size]) {
		state = regexComplete
	} else if size < regexHandLimit {
		for tile := range regexTileKinds {
			writeRegexTile(cards, size, tile)
			if m.orderedStatus(cards, size+1, key<<4|uint32(tile+1)) != regexRejected {
				state = regexContinuing
				break
			}
		}
	}
	m.orderedStates[key] = state
	return state
}

// unorderedStatus 按多重集缓存补牌状态，每个集合只检查其不重复的全排列。
func (m *regexMatcher) unorderedStatus(counts *[regexTileKinds]uint8, cards *[2 * regexHandLimit]byte, size int, key uint64) regexMatchState {
	if cached := m.unorderedStates[key]; cached != regexUnknown {
		return cached
	}
	state := regexRejected
	if size > 0 && m.matchesPermutation(counts, cards, size, 0) {
		state = regexComplete
	} else if size < regexHandLimit {
		for tile := range regexTileKinds {
			counts[tile]++
			next := m.unorderedStatus(counts, cards, size+1, key+uint64(1)<<(3*tile))
			counts[tile]--
			if next != regexRejected {
				state = regexContinuing
				break
			}
		}
	}
	m.unorderedStates[key] = state
	return state
}

func (m *regexMatcher) matchesPermutation(counts *[regexTileKinds]uint8, cards *[2 * regexHandLimit]byte, size, slot int) bool {
	if slot == size {
		return m.fullMatch(cards[:2*size])
	}
	for tile, count := range counts {
		if count == 0 {
			continue
		}
		counts[tile]--
		writeRegexTile(cards, slot, tile)
		matched := m.matchesPermutation(counts, cards, size, slot+1)
		counts[tile]++
		if matched {
			return true
		}
	}
	return false
}
