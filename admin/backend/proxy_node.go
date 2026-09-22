package main

import (
	"regexp"
	"strings"
)

// Subscription providers sometimes encode quota, expiry, reset and website
// notices as syntactically valid proxy nodes. They are metadata, not usable
// exits: allowing them into include-all groups can make url-test select one and
// send real client traffic through it.
var subscriptionMetadataNodePattern = regexp.MustCompile(
	`(?i)剩余|流量\s*[:：]|到期|过期|有效期|重置|官网|订阅\s*[:：]|套餐|余额|` +
		`(traffic|quota)\s*(left|remain(ing)?)|expir(e[sd]?|y)|` +
		`reset\s*(date|day)|official\s*(site|website)`,
)

func isSubscriptionMetadataNode(name string) bool {
	return subscriptionMetadataNodePattern.MatchString(strings.TrimSpace(name))
}
