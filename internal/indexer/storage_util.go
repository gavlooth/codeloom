package indexer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
)

// embeddingWorkerCount controls parallelism for embedding generation
// Set to 4 to balance throughput vs overwhelming the embedding service
const embeddingWorkerCount = 4

// StoreNodeWithEmbedding generates an embedding (if available) and stores a node in the graph
func StoreNodeWithEmbedding(
	ctx context.Context,
	node *parser.CodeNode,
	storage *graph.Storage,
	embProvider embedding.Provider,
) error {
	var emb []float32
	var err error

	if embProvider != nil && node.Content != "" {
		emb, err = embProvider.EmbedSingle(ctx, node.Content)
		if err != nil {
			log.Printf("Warning: embedding failed for %s: %v", node.ID, err)
			emb = nil
		}
	}

	graphNode := &graph.CodeNode{
		ID:          node.ID,
		Name:        node.Name,
		NodeType:    graph.NodeType(node.NodeType),
		Language:    string(node.Language),
		FilePath:    node.FilePath,
		StartLine:   node.StartLine,
		EndLine:     node.EndLine,
		Content:     node.Content,
		DocComment:  node.DocComment,
		Annotations: node.Annotations,
		Embedding:   emb,
	}

	if err := storage.UpsertNode(ctx, graphNode); err != nil {
		return fmt.Errorf("failed to store node %s: %w", node.ID, err)
	}

	return nil
}

// embeddingBatch represents a batch of nodes to embed
type embeddingBatch struct {
	batchIndex  int // Order in original sequence
	nodes       []parser.CodeNode
	texts       []string // Texts to embed
	textIndices []int    // Maps text index back to node index
}

// embeddingResult holds the result of embedding a batch
type embeddingResult struct {
	batchIndex int
	embeddings [][]float32
	err        error
}

// StoreNodesBatch generates embeddings in batches and stores multiple nodes
// Uses parallel embedding generation and batch database inserts for better performance
func StoreNodesBatch(
	ctx context.Context,
	nodes []parser.CodeNode,
	storage *graph.Storage,
	embProvider embedding.Provider,
	progress func(int),
) error {
	if len(nodes) == 0 {
		return nil
	}

	// If no embedding provider, just store nodes without embeddings
	if embProvider == nil {
		return storeNodesWithoutEmbeddings(ctx, nodes, storage, progress)
	}

	const batchSize = 100

	// Prepare all batches upfront
	var batches []embeddingBatch
	for batchStart := 0; batchStart < len(nodes); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(nodes) {
			batchEnd = len(nodes)
		}
		batch := nodes[batchStart:batchEnd]

		// Collect texts and track which nodes have content
		var texts []string
		var textIndices []int

		for i, node := range batch {
			if node.Content != "" {
				texts = append(texts, node.Content)
				textIndices = append(textIndices, i)
			}
		}

		batches = append(batches, embeddingBatch{
			batchIndex:  len(batches),
			nodes:       batch,
			texts:       texts,
			textIndices: textIndices,
		})
	}

	// Generate embeddings in parallel using worker pool
	embeddingResults := make([][][]float32, len(batches))
	textIndicesResults := make([][]int, len(batches))

	// Channel to distribute work
	workCh := make(chan embeddingBatch, len(batches))
	resultCh := make(chan embeddingResult, len(batches))

	// Start worker pool
	var wg sync.WaitGroup
	for w := 0; w < embeddingWorkerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range workCh {
				var embeddings [][]float32
				var err error

				if len(batch.texts) > 0 {
					embeddings, err = embProvider.Embed(ctx, batch.texts)
					if err != nil {
						log.Printf("Warning: batch %d embedding failed: %v", batch.batchIndex, err)
					}
				}

				resultCh <- embeddingResult{
					batchIndex: batch.batchIndex,
					embeddings: embeddings,
					err:        err,
				}
			}
		}()
	}

	// Send all batches to workers
	go func() {
		for _, batch := range batches {
			workCh <- batch
		}
		close(workCh)
	}()

	// Collect results in background
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Gather all embedding results
	for result := range resultCh {
		embeddingResults[result.batchIndex] = result.embeddings
		textIndicesResults[result.batchIndex] = batches[result.batchIndex].textIndices
	}

	// Now store all nodes with embeddings (sequential DB writes for consistency)
	var processedCount int32
	for i, batch := range batches {
		embeddings := embeddingResults[i]
		textIndices := textIndicesResults[i]

		// Build batch of graph nodes
		graphNodes := make([]*graph.CodeNode, len(batch.nodes))
		for j, node := range batch.nodes {
			var emb []float32

			// Find embedding for this node if it had content
			if embeddings != nil {
				for k, textIdx := range textIndices {
					if textIdx == j && k < len(embeddings) {
						emb = embeddings[k]
						break
					}
				}
			}

			graphNodes[j] = &graph.CodeNode{
				ID:          node.ID,
				Name:        node.Name,
				NodeType:    graph.NodeType(node.NodeType),
				Language:    string(node.Language),
				FilePath:    node.FilePath,
				StartLine:   node.StartLine,
				EndLine:     node.EndLine,
				Content:     node.Content,
				DocComment:  node.DocComment,
				Annotations: node.Annotations,
				Embedding:   emb,
			}
		}

		// Batch insert all nodes at once
		if err := storage.UpsertNodesBatch(ctx, graphNodes); err != nil {
			return fmt.Errorf("failed to batch store nodes: %w", err)
		}

		// Report progress
		atomic.AddInt32(&processedCount, int32(len(batch.nodes)))
		if progress != nil {
			progress(int(atomic.LoadInt32(&processedCount)))
		}
	}

	return nil
}

// storeNodesWithoutEmbeddings stores nodes when no embedding provider is available
// Uses batch inserts for better performance
func storeNodesWithoutEmbeddings(
	ctx context.Context,
	nodes []parser.CodeNode,
	storage *graph.Storage,
	progress func(int),
) error {
	const batchSize = 100

	for batchStart := 0; batchStart < len(nodes); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(nodes) {
			batchEnd = len(nodes)
		}
		batch := nodes[batchStart:batchEnd]

		// Build batch of graph nodes
		graphNodes := make([]*graph.CodeNode, len(batch))
		for i, node := range batch {
			graphNodes[i] = &graph.CodeNode{
				ID:          node.ID,
				Name:        node.Name,
				NodeType:    graph.NodeType(node.NodeType),
				Language:    string(node.Language),
				FilePath:    node.FilePath,
				StartLine:   node.StartLine,
				EndLine:     node.EndLine,
				Content:     node.Content,
				DocComment:  node.DocComment,
				Annotations: node.Annotations,
			}
		}

		// Batch insert
		if err := storage.UpsertNodesBatch(ctx, graphNodes); err != nil {
			return fmt.Errorf("failed to batch store nodes: %w", err)
		}

		if progress != nil {
			progress(batchEnd)
		}
	}
	return nil
}

// StoreEdgesBatch stores multiple edges in the graph using batch inserts
func StoreEdgesBatch(
	ctx context.Context,
	edges []parser.CodeEdge,
	storage *graph.Storage,
) error {
	if len(edges) == 0 {
		return nil
	}

	const batchSize = 500 // Edges are smaller, can batch more

	// Filter out invalid edges first
	validEdges := make([]parser.CodeEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.FromID == "" || edge.ToID == "" {
			log.Printf("Warning: skipping edge with empty IDs: %+v", edge)
			continue
		}
		validEdges = append(validEdges, edge)
	}

	// Process in batches
	for batchStart := 0; batchStart < len(validEdges); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(validEdges) {
			batchEnd = len(validEdges)
		}
		batch := validEdges[batchStart:batchEnd]

		// Build batch of graph edges
		graphEdges := make([]*graph.CodeEdge, len(batch))
		for i, edge := range batch {
			graphEdges[i] = &graph.CodeEdge{
				ID:       fmt.Sprintf("%s->%s:%s", edge.FromID, edge.ToID, edge.EdgeType),
				FromID:   edge.FromID,
				ToID:     edge.ToID,
				EdgeType: graph.EdgeType(edge.EdgeType),
				Weight:   1.0,
			}
		}

		// Batch insert
		if err := storage.UpsertEdgesBatch(ctx, graphEdges); err != nil {
			return fmt.Errorf("failed to batch store edges: %w", err)
		}
	}

	return nil
}
