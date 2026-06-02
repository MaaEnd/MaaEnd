package achievement

const (
	achievementStorageFileName = "Achievements.json"
	currentSchemaVersion       = 1

	eventOpenMXU = "mxu_open"

	firstOpenMXUAchievementID = "first_open_mxu"
)

type storageFile struct {
	SchemaVersion        int                       `json:"schema_version"`
	Achievements         map[string]achievementLog `json:"achievements"`
	Events               []eventLog                `json:"events"`
	RecentUnlocks        []unlockLog               `json:"recent_unlocks"`
	PendingNotifications []unlockLog               `json:"pending_notifications"`
	UpdatedAt            string                    `json:"updated_at"`
}

type achievementLog struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Event      string `json:"event"`
	Progress   int    `json:"progress"`
	Target     int    `json:"target"`
	UnlockedAt string `json:"unlocked_at,omitempty"`
}

type eventLog struct {
	Event     string `json:"event"`
	Increment int    `json:"increment"`
	DedupeKey string `json:"dedupe_key,omitempty"`
	UTCTime   string `json:"utc_time"`
}

type unlockLog struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	UnlockedAt string `json:"unlocked_at"`
}

type trackerParam struct {
	Event     string `json:"event"`
	Increment int    `json:"increment,omitempty"`
	DedupeKey string `json:"dedupe_key,omitempty"`
}

type reportParam struct {
	Limit int `json:"limit,omitempty"`
}

type rule struct {
	ID     string
	Title  string
	Event  string
	Target int
}
