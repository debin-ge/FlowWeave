package graph

import (
	"fmt"

	"flowweave/internal/domain/workflow/node"
)

// Builder 图的流式构建器，主要用于编程式构建和测试
type Builder struct {
	nodes     []node.Node
	nodesMap  map[string]node.Node
	edges     []*Edge
	edgeCount int
}

// NewBuilder 创建图构建器
func NewBuilder() *Builder {
	return &Builder{
		nodesMap: make(map[string]node.Node),
	}
}

// AddRoot 注册根节点（必须且仅调用一次）
func (b *Builder) AddRoot(n node.Node) *Builder {
	if len(b.nodes) > 0 {
		panic("root node has already been added")
	}
	b.registerNode(n)
	b.nodes = append(b.nodes, n)
	return b
}

// AddNode 添加节点并从指定前驱节点连接
// 如果 fromNodeID 为空，默认从最后添加的节点连接
func (b *Builder) AddNode(n node.Node, fromNodeID string, sourceHandle string) *Builder {
	if len(b.nodes) == 0 {
		panic("root node must be added before adding other nodes")
	}

	predecessorID := fromNodeID
	if predecessorID == "" {
		predecessorID = b.nodes[len(b.nodes)-1].ID()
	}

	if _, ok := b.nodesMap[predecessorID]; !ok {
		panic(fmt.Sprintf("predecessor node '%s' not found", predecessorID))
	}

	b.registerNode(n)
	b.nodes = append(b.nodes, n)

	if sourceHandle == "" {
		sourceHandle = "source"
	}

	edgeID := fmt.Sprintf("edge_%d", b.edgeCount)
	b.edgeCount++
	edge := NewEdgeWithID(edgeID, predecessorID, n.ID(), sourceHandle)
	b.edges = append(b.edges, edge)

	return b
}

// Connect 连接两个已存在的节点（不添加新节点）
func (b *Builder) Connect(tail, head, sourceHandle string) *Builder {
	if _, ok := b.nodesMap[tail]; !ok {
		panic(fmt.Sprintf("tail node '%s' not found", tail))
	}
	if _, ok := b.nodesMap[head]; !ok {
		panic(fmt.Sprintf("head node '%s' not found", head))
	}

	if sourceHandle == "" {
		sourceHandle = "source"
	}

	edgeID := fmt.Sprintf("edge_%d", b.edgeCount)
	b.edgeCount++
	edge := NewEdgeWithID(edgeID, tail, head, sourceHandle)
	b.edges = append(b.edges, edge)

	return b
}

// Build 构建并返回 Graph 实例
func (b *Builder) Build() *Graph {
	if len(b.nodes) == 0 {
		panic("cannot build an empty graph")
	}

	nodes := make(map[string]node.Node, len(b.nodes))
	for _, n := range b.nodes {
		nodes[n.ID()] = n
	}

	edges := make(map[string]*Edge, len(b.edges))
	inEdges := make(map[string][]string)
	outEdges := make(map[string][]string)

	for _, edge := range b.edges {
		edges[edge.ID] = edge
		outEdges[edge.Tail] = append(outEdges[edge.Tail], edge.ID)
		inEdges[edge.Head] = append(inEdges[edge.Head], edge.ID)
	}

	return &Graph{
		Nodes:    nodes,
		Edges:    edges,
		InEdges:  inEdges,
		OutEdges: outEdges,
		RootNode: b.nodes[0],
	}
}

func (b *Builder) registerNode(n node.Node) {
	if n.ID() == "" {
		panic("node must have a non-empty id")
	}
	if _, ok := b.nodesMap[n.ID()]; ok {
		panic(fmt.Sprintf("duplicate node id detected: %s", n.ID()))
	}
	b.nodesMap[n.ID()] = n
}
