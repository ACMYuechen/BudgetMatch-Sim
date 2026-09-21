package recommend

import (
	"math/big"
	"regexp"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
)

// 这是有界的约束语法，不把型号、功率或数量当作金额，也不猜测汇率。
// 先完整识别数值 token，再验证格式，避免把 1,00 / 1.2.3 / 1e3 的子串当合法金额。
const (
	numberExpression        = `[+\-负]?\s*(?:[0-9]+(?:[.,][0-9]+)*|\.[0-9]+)(?:[eE][+\-]?[0-9]+)?`
	chineseNumberExpression = `[负零〇一二两三四五六七八九十百千万亿]+`
	currencyExpression      = `人民币|美元|美金|欧元|日元|港元|港币|英镑|韩元|澳元|加元|新加坡元|新台币|台币|卢布|卢比|円|块钱|元|块|rmb\b|cny\b|yuan\b|usd\b|dollars?\b|eur\b|euros?\b|jpy\b|yen\b|hkd\b|gbp\b|pounds?\b|krw\b|aud\b|cad\b|sgd\b|twd\b|rub\b|inr\b|\p{Sc}`
	budgetMarkerExpression  = `预算\s*(?:为|是|在|约|大概|控制在|不超过|不要超过|不超|最多|至少|不少于|不低于|最低|上限(?:为|是)?|改成|(?:降低|减少|下调|降)(?:到|至|为)|(?:提高|增加|加|调整|改)(?:到|至|为)?|under|below)?` +
		`|budget(?:\s+(?:is|around|about|under|below|within|up\s+to|max(?:imum)?|at\s+least))?` +
		`|不超过|不要超过|不超|最多|上限(?:为|是)?|至少|不少于|不低于|最低|起码|(?:改|调整)(?:到|至|为)` +
		`|under|below|within|around|about|up\s+to|no\s+more\s+than|less\s+than|at\s+least|max(?:imum)?\s+budget`
	budgetSuffixExpression   = `以内|左右|上下|以下|之内|封顶|预算|以上|起步|起|or\s+less|or\s+so`
	quantityMarkerExpression = `最多|至多|不超过|不要超过|不超|至少|不少于|不低于|最低|起码|总共|一共|数量(?:为|是)?|只(?:买|要|选)|买|选|at\s+most|up\s+to|no\s+more\s+than|at\s+least|max(?:imum)?|only`
)

func moneyExpression(name string) string {
	return `(?P<` + name + `sign>[+\-负]?)\s*(?P<` + name + `pre>` + strings.ReplaceAll(currencyExpression, `\b`, "") + `)?\s*` +
		`(?P<` + name + `number>` + numberExpression + `|` + chineseNumberExpression + `)\s*` +
		`(?P<` + name + `scale>k|w|千|万|百|亿)?\s*(?P<` + name + `post>` + currencyExpression + `)?`
}

var (
	budgetRangePattern = regexp.MustCompile(`(?i)(?P<marker>` + budgetMarkerExpression + `)?\s*[:：]?\s*` +
		moneyExpression("left") + `\s*(?P<separator>[-~～—–]|至|到|to\b|或者|或|还是|or\b)\s*` + moneyExpression("right") + `\s*(?P<suffix>` + budgetSuffixExpression + `)?`)
	budgetValuePattern = regexp.MustCompile(`(?i)(?P<marker>` + budgetMarkerExpression + `)?\s*[:：]?\s*` +
		moneyExpression("value") + `\s*(?P<suffix>` + budgetSuffixExpression + `)?`)
	quantityPattern = regexp.MustCompile(`(?i)(?P<marker>` + quantityMarkerExpression + `)?\s*` +
		`(?P<count>[+\-负]?\s*(?:` + numberExpression + `|` + chineseNumberExpression + `|zero|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)` +
		`(?:\s*[-~～到至]\s*(?:` + numberExpression + `|` + chineseNumberExpression + `))?)\s*` +
		`(?P<unit>个|件|台|部|套|副|只|items?\b|pieces?\b|units?\b)\s*(?P<suffix>以内|以下|以上|起步|起)?`)
	decimalAmountPattern   = regexp.MustCompile(`^\+?(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)(?:\.[0-9]+)?$`)
	integerCountPattern    = regexp.MustCompile(`^\+?[0-9]+$`)
	budgetKeywordPattern   = regexp.MustCompile(`预算|\bbudget\b`)
	unchangedBudgetPattern = regexp.MustCompile(`预算(?:保持)?不变|(?:保持|沿用)(?:原来|原有|之前|上轮|上一轮)?(?:的)?预算|(?:same|unchanged)\s+budget|budget\s+(?:is\s+)?unchanged`)
)

type textLimit struct {
	value    int64
	explicit bool
	err      error
}

// namedMatch 保留完整匹配的边界，避免在多个扫描阶段重复消费同一金额。
type namedMatch struct {
	query   string
	pattern *regexp.Regexp
	indices []int
}

func (m namedMatch) get(name string) string {
	i := m.pattern.SubexpIndex(name) * 2
	if i < 0 || m.indices[i] < 0 {
		return ""
	}
	return m.query[m.indices[i]:m.indices[i+1]]
}

func (m namedMatch) embeddedInWord() bool {
	start := m.indices[0]
	// 英文/数字不能从型号或单词中间开始，中文不使用 ASCII 的单词边界。
	return start > 0 && isASCIIWordByte(m.query[start-1]) && isASCIIWordByte(m.query[start])
}

func normalizeConstraintText(query string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r == '−' || r == '－':
			return '-'
		case r == '＋':
			return '+'
		case r == '．':
			return '.'
		default:
			return r
		}
	}, query))
}

func maskText(query string, spans [][2]int) string {
	masked := []byte(query)
	for _, span := range spans {
		for i := span[0]; i < span[1]; i++ {
			masked[i] = ' '
		}
	}
	return string(masked)
}

func hasLowerBound(marker, suffix string) bool {
	marker = strings.Join(strings.Fields(marker), " ")
	return strings.Contains(marker, "至少") || strings.Contains(marker, "不少于") ||
		strings.Contains(marker, "不低于") || strings.Contains(marker, "最低") || strings.Contains(marker, "起码") ||
		strings.Contains(marker, "at least") || suffix == "以上" || strings.HasPrefix(suffix, "起")
}

// chooseTextLimit 只在同一优先级内比较；预算限定语优先于商品标价。
// 不擅自选择第一个/最后一个冲突值，也不把多个商品数量相加。
func chooseTextLimit(limits []textLimit, invalid error) (int64, error) {
	explicit := false
	for _, limit := range limits {
		explicit = explicit || limit.explicit
	}
	var result int64
	for _, limit := range limits {
		if explicit && !limit.explicit {
			continue
		}
		if limit.err != nil {
			return 0, limit.err
		}
		if result != 0 && result != limit.value {
			return 0, invalid
		}
		result = limit.value
	}
	return result, nil
}

// parseTextQuantities 同时移除数量表达，预算扫描不能再把“最多 2 件”当金额。
func parseTextQuantities(query string) (int32, string, error) {
	var limits []textLimit
	var spans [][2]int
	for _, indices := range quantityPattern.FindAllStringSubmatchIndex(query, -1) {
		m := namedMatch{query, quantityPattern, indices}
		if m.embeddedInWord() {
			continue
		}
		spans = append(spans, [2]int{indices[0], indices[1]})
		marker, suffix := m.get("marker"), m.get("suffix")
		// “一套桌面”“三件套”是组合/商品描述，不等于推荐条目数上限。
		explicit := marker != "" && marker != "买" && marker != "选" || suffix != ""
		if !explicit && (m.get("unit") == "套" || strings.HasPrefix(query[indices[1]:], "套")) {
			continue
		}
		value, err := parseCount(strings.TrimSpace(m.get("count")))
		if hasLowerBound(marker, suffix) {
			err = agent.ErrItemLimitText
		}
		limits = append(limits, textLimit{value: value, explicit: explicit, err: err})
	}
	// 多个按商品描述的数量没有明确总上限时不能猜测求和或只取其中一个。
	if len(limits) > 1 {
		hasExplicit := false
		for _, limit := range limits {
			hasExplicit = hasExplicit || limit.explicit
		}
		if !hasExplicit {
			return 0, maskText(query, spans), agent.ErrItemLimitText
		}
	}
	value, err := chooseTextLimit(limits, agent.ErrItemLimitText)
	return int32(value), maskText(query, spans), err
}

func parseCount(value string) (int64, error) {
	switch value {
	case "一", "one":
		return 1, nil
	case "二", "两", "two":
		return 2, nil
	case "三", "three":
		return 3, nil
	case "四", "four":
		return 4, nil
	case "五", "five":
		return 5, nil
	case "六", "six":
		return 6, nil
	case "七", "seven":
		return 7, nil
	case "八", "eight":
		return 8, nil
	case "九", "nine":
		return 9, nil
	case "十", "ten":
		return 10, nil
	}
	if integerCountPattern.MatchString(value) {
		n, ok := new(big.Int).SetString(strings.TrimPrefix(value, "+"), 10)
		if ok && n.Sign() > 0 && n.Cmp(big.NewInt(int64(agent.MaxItems))) <= 0 {
			return n.Int64(), nil
		}
	}
	return 0, agent.ErrItemLimitText
}

func parseTextBudget(query string) (int64, error) {
	var limits []textLimit
	var spans [][2]int
	for _, indices := range budgetRangePattern.FindAllStringSubmatchIndex(query, -1) {
		m := namedMatch{query, budgetRangePattern, indices}
		if m.embeddedInWord() {
			continue
		}
		explicit := m.get("marker") != "" || m.get("suffix") != ""
		spans = append(spans, [2]int{indices[0], indices[1]})
		if !explicit && !moneySignal(m, "left") && !moneySignal(m, "right") {
			continue
		}
		leftScale, rightScale := m.get("leftscale"), m.get("rightscale")
		// 3-5k 继承量级；3k-5000元不把明确的元再次放大成千元。
		if leftScale == "" && m.get("leftpost") == "" && m.get("leftpre") == "" {
			leftScale = rightScale
		}
		if rightScale == "" && m.get("rightpost") == "" && m.get("rightpre") == "" {
			rightScale = leftScale
		}
		left, err := parseMoney(m, "left", leftScale)
		right, rightErr := parseMoney(m, "right", rightScale)
		if err == nil {
			err = rightErr
		}
		separator := m.get("separator")
		alternative := separator == "或" || separator == "或者" || separator == "还是" || separator == "or"
		if err == nil && (left > right || alternative || hasLowerBound(m.get("marker"), m.get("suffix"))) {
			err = agent.ErrBudgetText
		}
		limits = append(limits, textLimit{value: right, explicit: explicit, err: err})
	}
	query = maskText(query, spans)
	spans = nil
	for _, indices := range budgetValuePattern.FindAllStringSubmatchIndex(query, -1) {
		m := namedMatch{query, budgetValuePattern, indices}
		if m.embeddedInWord() {
			continue
		}
		explicit := m.get("marker") != "" || m.get("suffix") != ""
		// 单独 4K、65W 是商品参数；只有货币单位或预算限定语才赋予金额语义。
		if !explicit && m.get("valuepre") == "" && m.get("valuepost") == "" {
			continue
		}
		value, err := parseMoney(m, "value", m.get("valuescale"))
		if err == nil && hasLowerBound(m.get("marker"), m.get("suffix")) {
			err = agent.ErrBudgetText
		}
		limits = append(limits, textLimit{value: value, explicit: explicit, err: err})
		spans = append(spans, [2]int{indices[0], indices[1]})
	}
	// 出现预算限定词却没有完整解析（NaN、未知币种、取消预算等）不是“缺省”。
	remaining := unchangedBudgetPattern.ReplaceAllString(maskText(query, spans), "")
	if budgetKeywordPattern.MatchString(remaining) {
		return 0, agent.ErrBudgetText
	}
	return chooseTextLimit(limits, agent.ErrBudgetText)
}

func moneySignal(m namedMatch, name string) bool {
	return m.get(name+"pre") != "" || m.get(name+"post") != "" || m.get(name+"scale") != ""
}

func parseMoney(m namedMatch, name, scale string) (int64, error) {
	for _, currency := range []string{m.get(name + "pre"), m.get(name + "post")} {
		switch currency {
		case "", "人民币", "元", "块", "块钱", "rmb", "cny", "yuan", "¥", "￥":
		default:
			return 0, agent.ErrBudgetCurrency
		}
	}
	// 不接受金额 token 的有效前缀，如 500xyz 或 2keyboard 中的 2k。
	for _, field := range []string{"post", "scale", "number"} {
		if m.get(name+field) == "" {
			continue
		}
		end := m.indices[m.pattern.SubexpIndex(name+field)*2+1]
		if end < len(m.query) && isASCIIWordByte(m.query[end]) {
			return 0, agent.ErrBudgetText
		}
		break
	}
	if sign := m.get(name + "sign"); sign == "-" || sign == "负" {
		return 0, agent.ErrBudgetText
	}
	amount := strings.TrimSpace(m.get(name + "number"))
	if !decimalAmountPattern.MatchString(amount) {
		return 0, agent.ErrBudgetText
	}
	value, ok := new(big.Rat).SetString(strings.ReplaceAll(amount, ",", ""))
	if !ok || value.Sign() <= 0 {
		return 0, agent.ErrBudgetText
	}
	multiplier := int64(100)
	switch scale {
	case "":
	case "w", "万":
		multiplier *= 10000
	case "k", "千":
		multiplier *= 1000
	default:
		return 0, agent.ErrBudgetText
	}
	value.Mul(value, big.NewRat(multiplier, 1))
	if !value.IsInt() || value.Cmp(big.NewRat(agent.MaxBudgetCents, 1)) > 0 {
		return 0, agent.ErrBudgetText
	}
	return value.Num().Int64(), nil
}
