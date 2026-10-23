package autosell

import (
	"fmt"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
)

func TestFriendPriceRequiresOwnPriceAndThreshold(t *testing.T) {
	cases := []struct {
		name      string
		friend    int
		own       int
		threshold int
		matched   bool
	}{
		{name: "below home but above threshold", friend: 5100, own: 5300, threshold: 5000, matched: false},
		{name: "below threshold but above home", friend: 4954, own: 4800, threshold: 5000, matched: false},
		{name: "equal to home and threshold", friend: 5000, own: 5000, threshold: 5000, matched: true},
		{name: "equal to home above threshold", friend: 5300, own: 5300, threshold: 5000, matched: true},
		{name: "equal to threshold above home", friend: 5000, own: 4800, threshold: 5000, matched: true},
		{name: "above both", friend: 5500, own: 5300, threshold: 5000, matched: true},
	}
	for _, node := range []string{"AutoSellFriendsPriceRecognition", "AutoSellFriendsPriceCurrentRecognition"} {
		for _, tc := range cases {
			t.Run(node+"/"+tc.name, func(t *testing.T) {
				expression, _, err := boolexpr.ResolvePlaceholders(friendPriceExpression(node, tc.threshold, tc.own), func(name string) (int, error) {
					if name != node {
						return 0, fmt.Errorf("unexpected recognition node %s", name)
					}
					return tc.friend, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				matched, err := boolexpr.Evaluate(expression)
				if err != nil {
					t.Fatal(err)
				}
				if matched != tc.matched {
					t.Fatalf("%s = %v, want %v", expression, matched, tc.matched)
				}
			})
		}
	}
}
