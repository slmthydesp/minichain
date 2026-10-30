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
	peers  []string
	client *http.Client
}

func New(c *chain.Chain, peers []string) *Node {
	cleaned := make([]string, 0, len(peers))
	for _, peer := range peers {
		peer = strings.TrimRight(strings.TrimSpace(peer), "/")
		if peer != "" {
			cleaned = append(cleaned, peer)
		}
	}
	return &Node{
		chain:  c,
		peers:  cleaned,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func NewMiner(c *chain.Chain) *Node {
	return New(c, nil)
}

func NewFollower(c *chain.Chain, peer string) *Node {
	return New(c, []string{peer})
}

func (n *Node) Chain() *chain.Chain {
	return n.chain
}

func (n *Node) FollowOnce(ctx context.Context) error {
	if len(n.peers) == 0 {
		return fmt.Errorf("not a follower")
	}
	var first error
	for _, peer := range n.peers {
		if err := n.followPeer(ctx, peer); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (n *Node) followPeer(ctx context.Context, peer string) error {
	body, status, err := n.get(ctx, peer+"/status")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("status %d: %s", status, body)
	}
	var head statusJSON
	if err := json.Unmarshal(body, &head); err != nil {
		return err
	}
	hash, err := chain.ParseHash(head.Head)
	if err != nil {
		return err
	}
	seen := map[chain.Hash]bool{}
	var pending []chain.Block
	for !n.chain.Has(hash) {
		if seen[hash] {
			return fmt.Errorf("parent cycle")
		}
		seen[hash] = true
		block, err := n.fetchBlock(ctx, peer, hash)
		if err != nil {
			return err
		}
		pending = append(pending, block)
		if block.Height == 0 {
			break
		}
		hash = block.ParentHash
	}
	for i := len(pending) - 1; i >= 0; i-- {
		if err := n.chain.Append(pending[i]); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) fetchBlock(ctx context.Context, peer string, hash chain.Hash) (chain.Block, error) {
	body, status, err := n.get(ctx, peer+"/block?hash="+hash.Hex())
	if err != nil {
		return chain.Block{}, err
	}
	if status != http.StatusOK {
		return chain.Block{}, fmt.Errorf("block status %d: %s", status, body)
	}
	var item blockJSON
	if err := json.Unmarshal(body, &item); err != nil {
		return chain.Block{}, err
	}
	return decodeBlock(item)
}

func (n *Node) get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
