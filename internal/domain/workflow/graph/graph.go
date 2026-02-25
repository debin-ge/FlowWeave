package graph

import (
	"encoding/json"
	"fmt"

	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// Graph 工作流图，包含节点和边的有向图结构
type Graph struct {
	Nodes    map[string]node.Node // node_id -> Node
	Edges    map[string]*Edge     // edge_id -> Edge
	InEdges  map[string][]string  // node_id -> []edge_id (入边)
	OutEdges map[string][]string  // node_id -> []edge_id (出边)
	RootNode node.Node            // 根节点 (Start)
}

// Init 从配置初始化图
func Init(config *types.GraphConfig, factory *node.Factory) (*Graph, error) {
	if len(config.Nodes) == 0 {
		return nil, fmt.Errorf("graph must have at least one node")
	}

	// 1. 过滤掉 custom-note 类型节点
	filteredNodes := make([]types.NodeConfig, 0, len(config.Nodes))
	for _, nc := range config.Nodes {
		if nc.Type != "custom-note" {
			filteredNodes = append(filteredNodes, nc)
		}
	}

	// 2. 构建 node_id -> config 映射
	nodeConfigsMap := make(map[string]types.NodeConfig, len(filteredNodes))
	for _, nc := range filteredNodes {
		nodeConfigsMap[nc.ID] = nc
	}

	// 3. 查找根节点
	rootNodeID, err := findRootNodeID(nodeConfigsMap, config.Edges)
	if err != nil {
		return nil, err
	}

	// 4. 构建边
	edges, inEdges, outEdges := buildEdges(config.Edges)

	// 5. 创建节点实例
	nodes, err := createNodeInstances(nodeConfigsMap, factory)
	if err != nil {
		return nil, err
	}

	// 6. 提升 fail-branch 节点
	promoteFailBranchNodes(nodes)

	// 7. 获取根节点
	rootNode, ok := nodes[rootNodeID]
	if !ok {
		return nil, fmt.Errorf("root node %s not found in created nodes", rootNodeID)
	}

	// 8. 标记非活跃根分支
	markInactiveRootBranches(nodes, edges, inEdges, outEdges, rootNodeID)

	return &Graph{
		Nodes:    nodes,
		Edges:    edges,
		InEdges:  inEdges,
		OutEdges: outEdges,
		RootNode: rootNode,
	}, nil
}

// findRootNodeID 查找根节点 ID（没有入边的节点，优先选择 START 类型）
func findRootNodeID(nodeConfigs map[string]types.NodeConfig, edgeConfigs []types.EdgeConfig) (string, error) {
	// 收集有入边的节点
	nodesWithIncoming := make(map[string]bool)
	for _, ec := range edgeConfigs {
		if ec.Target != "" {
			nodesWithIncoming[ec.Target] = true
		}
	}

	// 找没有入边的候选根节点
	var candidates []string
	for nid := range nodeConfigs {
		if !nodesWithIncoming[nid] {
			candidates = append(candidates, nid)
		}
	}

	// 优先选择 START 类型节点
	for _, nid := range candidates {
		nc := nodeConfigs[nid]
		var nodeData types.NodeData
		if err := parseNodeData(nc.Data, &nodeData); err == nil {
			nt := types.NodeType(nodeData.Type)
			if nt.IsStartNode() {
				return nid, nil
			}
		}
	}

	if len(candidates) > 0 {
		return candidates[0], nil
	}

	return "", fmt.Errorf("unable to determine root node ID")
}

// buildEdges 从配置构建 Edge 对象和邻接表
func buildEdges(edgeConfigs []types.EdgeConfig) (map[string]*Edge, map[string][]string, map[string][]string) {
	edges := make(map[string]*Edge)
	inEdges := make(map[string][]string)
	outEdges := make(map[string][]string)

	for i, ec := range edgeConfigs {
		if ec.Source == "" || ec.Target == "" {
			continue
		}

		edgeID := fmt.Sprintf("edge_%d", i)
		sourceHandle := ec.SourceHandle
		if sourceHandle == "" {
			sourceHandle = "source"
		}

		edge := NewEdgeWithID(edgeID, ec.Source, ec.Target, sourceHandle)
		edges[edgeID] = edge
		outEdges[ec.Source] = append(outEdges[ec.Source], edgeID)
		inEdges[ec.Target] = append(inEdges[ec.Target], edgeID)
	}

	return edges, inEdges, outEdges
}

// createNodeInstances 通过工厂创建所有节点实例
func createNodeInstances(nodeConfigs map[string]types.NodeConfig, factory *node.Factory) (map[string]node.Node, error) {
	nodes := make(map[string]node.Node, len(nodeConfigs))

	for id, nc := range nodeConfigs {
		n, err := factory.CreateNode(nc)
		if err != nil {
			return nil, fmt.Errorf("failed to create node %s: %w", id, err)
		}
		nodes[id] = n
	}

	return nodes, nil
}

// promoteFailBranchNodes 将配置了 FAIL_BRANCH 策略的节点提升为 BRANCH 执行类型
func promoteFailBranchNodes(nodes map[string]node.Node) {
	for _, n := range nodes {
		if n.ErrorStrategy() == types.ErrorStrategyFailBranch {
			n.SetState(types.NodeStateUnknown) // 重置状态
		}
	}
}

// markInactiveRootBranches 标记非活跃根分支下游为 SKIPPED
func markInactiveRootBranches(
	nodes map[string]node.Node,
	edges map[string]*Edge,
	inEdges map[string][]string,
	outEdges map[string][]string,
	activeRootID string,
) {
	// 找所有顶层根节点 (ROOT 执行类型)
	var topLevelRoots []string
	for _, n := range nodes {
		if n.ExecutionType() == types.NodeExecutionTypeRoot {
			topLevelRoots = append(topLevelRoots, n.ID())
		}
	}

	if len(topLevelRoots) <= 1 {
		return
	}

	// 标记非活跃根节点及下游
	for _, rootID := range topLevelRoots {
		if rootID == activeRootID {
			continue
		}
		if n, ok := nodes[rootID]; ok {
			n.SetState(types.NodeStateSkipped)
			markDownstream(rootID, nodes, edges, inEdges, outEdges)
		}
	}
}

// markDownstream 递归标记下游节点和边为 SKIPPED
func markDownstream(
	nodeID string,
	nodes map[string]node.Node,
	edges map[string]*Edge,
	inEdges map[string][]string,
	outEdges map[string][]string,
) {
	n, ok := nodes[nodeID]
	if !ok || n.State() != types.NodeStateSkipped {
		return
	}

	for _, edgeID := range outEdges[nodeID] {
		edge := edges[edgeID]
		edge.SetState(types.NodeStateSkipped)

		targetNode, ok := nodes[edge.Head]
		if !ok {
			continue
		}

		// 检查目标节点的所有入边是否都被跳过
		allSkipped := true
		for _, inEdgeID := range inEdges[targetNode.ID()] {
			if edges[inEdgeID].GetState() != types.NodeStateSkipped {
				allSkipped = false
				break
			}
		}

		if allSkipped {
			targetNode.SetState(types.NodeStateSkipped)
			markDownstream(targetNode.ID(), nodes, edges, inEdges, outEdges)
		}
	}
}

// GetOutgoingEdges 获取节点的所有出边
func (g *Graph) GetOutgoingEdges(nodeID string) []*Edge {
	edgeIDs := g.OutEdges[nodeID]
	result := make([]*Edge, 0, len(edgeIDs))
	for _, eid := range edgeIDs {
		if edge, ok := g.Edges[eid]; ok {
			result = append(result, edge)
		}
	}
	return result
}

// GetIncomingEdges 获取节点的所有入边
func (g *Graph) GetIncomingEdges(nodeID string) []*Edge {
	edgeIDs := g.InEdges[nodeID]
	result := make([]*Edge, 0, len(edgeIDs))
	for _, eid := range edgeIDs {
		if edge, ok := g.Edges[eid]; ok {
			result = append(result, edge)
		}
	}
	return result
}

// NodeIDs 返回所有节点 ID 列表
func (g *Graph) NodeIDs() []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	return ids
}

// parseNodeData 从 json.RawMessage 解析节点数据
func parseNodeData(data []byte, out interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("empty node data")
	}
	return json.Unmarshal(data, out)
}
