package charactercontroller

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type DailyTasksMoveParam struct {
	Directions []string `json:"directions"`
	Queue      []string `json:"queue"`
	QueueKey   string   `json:"queue_key"`
}

var (
	queueIndex     = make(map[string]int)
	queueIndexLock sync.RWMutex
)

type DailyTasksMoveAction struct{}

func (a *DailyTasksMoveAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	duration := 200
	ctrl := ctx.GetTasker().GetController()

	var params DailyTasksMoveParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Warn().Err(err).Msg("failed to parse DailyTasksMoveParam, using defaults")
	}

	var dir string

	if len(params.Queue) > 0 {
		queueKey := params.QueueKey
		if queueKey == "" {
			queueKey = fmt.Sprintf("%v", params.Queue)
		}

		queueIndexLock.Lock()
		idx := queueIndex[queueKey]
		if idx >= len(params.Queue) {
			idx = 0
			queueIndex[queueKey] = idx
		}
		dir = params.Queue[idx]
		queueIndex[queueKey] = idx + 1
		queueIndexLock.Unlock()

		log.Debug().Str("queue_key", queueKey).Int("index", idx).Str("direction", dir).Msg("executing from queue")
	} else {
		directions := params.Directions
		if len(directions) == 0 {
			directions = []string{"forward", "backward", "left", "right"}
		}
		dir = directions[rand.Intn(len(directions))]
		log.Debug().Str("direction", dir).Msg("random direction selected")
	}

	switch dir {
	case "forward", "w", "W":
		moveAxis(ctx, duration)
		log.Debug().Str("direction", dir).Msg("moving forward")
	case "backward", "s", "S":
		moveAxis(ctx, -duration)
		log.Debug().Str("direction", dir).Msg("moving backward")
	case "left", "a", "A":
		ctrl.PostKeyDown(65).Wait()
		time.Sleep(time.Duration(duration) * time.Millisecond)
		ctrl.PostKeyUp(65).Wait()
		log.Debug().Str("direction", dir).Msg("moving left (A)")
	case "right", "d", "D":
		ctrl.PostKeyDown(68).Wait()
		time.Sleep(time.Duration(duration) * time.Millisecond)
		ctrl.PostKeyUp(68).Wait()
		log.Debug().Str("direction", dir).Msg("moving right (D)")
	}

	return true
}

var _ maa.CustomActionRunner = &DailyTasksMoveAction{}
