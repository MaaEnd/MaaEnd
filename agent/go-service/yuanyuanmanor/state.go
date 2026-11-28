package yuanyuanmanor

// MenuTab 表示菜单设定界面当前所处的分类页签。
type MenuTab string

const (
	TabEntree  MenuTab = "entree"
	TabSide    MenuTab = "side"
	TabDessert MenuTab = "dessert"
	TabSoup    MenuTab = "soup"
	TabDrinks  MenuTab = "drinks"
)

// MenuItem 记录菜单设定阶段选中的单道菜品或饮品信息。
type MenuItem struct {
	ID       string
	Tab      MenuTab
	Quantity int
}

// InnkeepingState 保存客栈经营在 Go 侧维护的菜单与页签状态。
// 菜品信息在首轮菜单设定时记录，后续轮次直接沿用，无需重置。
type InnkeepingState struct {
	currentTab    MenuTab
	tabStartCount int
	cardIndex     int
	dishes        []MenuItem
	drinks        []MenuItem
}

var innkeepingState = &InnkeepingState{}

func (s *InnkeepingState) onTabRecognized(tab MenuTab, count int) {
	if s.currentTab != tab || count < s.tabStartCount {
		s.currentTab = tab
		s.tabStartCount = count
		s.cardIndex = 0
	}
}

func (s *InnkeepingState) pickedInCurrentTab(count int) int {
	return count - s.tabStartCount
}

func (s *InnkeepingState) nextCardIndex() int {
	idx := s.cardIndex
	s.cardIndex++
	return idx
}

func (s *InnkeepingState) checkedCardNumber() int {
	return s.cardIndex
}

func (s *InnkeepingState) recordMenuItem(tab MenuTab, id string, quantity int) {
	item := MenuItem{
		ID:       id,
		Tab:      tab,
		Quantity: quantity,
	}
	if tab == TabDrinks {
		s.drinks = upsertMenuItem(s.drinks, item)
		return
	}
	s.dishes = upsertMenuItem(s.dishes, item)
}

func upsertMenuItem(items []MenuItem, item MenuItem) []MenuItem {
	for i := range items {
		if items[i].ID == item.ID {
			items[i] = item
			return items
		}
	}
	return append(items, item)
}

// Dishes 返回已记录的菜品列表副本。
func (s *InnkeepingState) Dishes() []MenuItem {
	out := make([]MenuItem, len(s.dishes))
	copy(out, s.dishes)
	return out
}

// Drinks 返回已记录的饮品列表副本。
func (s *InnkeepingState) Drinks() []MenuItem {
	out := make([]MenuItem, len(s.drinks))
	copy(out, s.drinks)
	return out
}

// DishIDs 返回按选中顺序排列的菜品 ID 列表。
func (s *InnkeepingState) DishIDs() []string {
	ids := make([]string, len(s.dishes))
	for i, item := range s.dishes {
		ids[i] = item.ID
	}
	return ids
}

// DrinkIDs 返回按选中顺序排列的饮品 ID 列表。
func (s *InnkeepingState) DrinkIDs() []string {
	ids := make([]string, len(s.drinks))
	for i, item := range s.drinks {
		ids[i] = item.ID
	}
	return ids
}
