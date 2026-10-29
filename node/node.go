package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/slmthydesp/minichain/chain"
)

type Node struct {
	chain  *chain.Chain
	peer   string
	client *http.Client
}

func NewMiner(c *chain.Chain) *Node {
	return &Node{chain: c, client: http.DefaultClient}
}

func NewFollower(c *chain.Chain, peer string) *Node {
	return &Node{
		chain:  c,
		peer:   strings.TrimRight(peer, "/"),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (n *Node) Chain() *chain.Chain {
	return n.chain
}

func (n *Node) FollowOnce(ctx context.Context) error {
	if n.peer == "" {
		return fmt.Errorf("not a follower")
	}
	url := fmt.Sprintf("%s/blocks?after=%d", n.peer, n.chain.Head().Height)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("blocks status %d: %s", resp.StatusCode, body)
	}
	var blocks []blockJSON
	if err := json.Unmarshal(body, &blocks); err != nil {
		return err
	}
	for _, item := range blocks {
		block, err := decodeBlock(item)
		if err != nil {
			return err
		}
		if err := n.chain.Append(block); err != nil {
			return err
		}
	}
	return nil
}
