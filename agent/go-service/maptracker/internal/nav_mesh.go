// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

const (
	// NavMeshDataPath stores MapTracker NavMesh files under the resource root.
	NavMeshDataPath = "data/MapTrackerNavMesh"

	navMeshHeader          = "MapTrackerNavMesh"
	navMeshVersion         = 1
	navMeshEncoding        = "UTF-8"
	navMeshSectionMeta     = navMeshHeader + ".Meta"
	navMeshSectionVertices = navMeshHeader + ".Vertices"
	navMeshSectionEdges    = navMeshHeader + ".Edges"

	NavMeshVertexFlagTeleportAnchor = 1
	NavMeshVertexFlagHidden         = 2
	NavMeshVertexFlagSystem         = 4
	NavMeshVertexFlagRare           = 8
	NavMeshVertexFlagCollectable    = 16
	NavMeshVertexFlagDigable        = 32
)

var (
	navMeshSectionRegexp  = regexp.MustCompile(`^\s*\[(?P<section>[^\]]+)\]\s*$`)
	navMeshKeyValueRegexp = regexp.MustCompile(
		`^\s*(?P<key>[A-Za-z][A-Za-z0-9_]*)\s*=\s*(?P<value>.*?)\s*$`,
	)
	navMeshFloatPattern  = `[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?`
	navMeshPosIntPattern = `[1-9]\d*`
	navMeshIntPattern    = `[-+]?\d+`
	navMeshVertexRegexp  = regexp.MustCompile(
		`^\s*V(?P<id>` + navMeshPosIntPattern + `)\s*=\s*X(?P<x>` + navMeshFloatPattern + `)\s*,\s*Y(?P<y>` + navMeshFloatPattern + `)\s*,\s*T(?P<t>` + navMeshIntPattern + `)\s*,\s*E(?P<e>` + navMeshIntPattern + `)\s*,\s*F\((?P<flags>[A-Za-z]*)\)\s*$`,
	)
	navMeshEdgeRegexp = regexp.MustCompile(
		`^\s*E(?P<id>` + navMeshPosIntPattern + `)\s*=\s*S(?P<source>` + navMeshPosIntPattern + `)\s*,\s*D(?P<destination>` + navMeshPosIntPattern + `)\s*,\s*B(?P<bidirectional>[01])\s*,\s*C(?P<cost>` + navMeshFloatPattern + `)\s*,\s*F\((?P<flags>[A-Za-z]*)\)\s*$`,
	)
)

// NavMeshMeta stores metadata from a MapTracker NavMesh file.
type NavMeshMeta struct {
	Version       int
	Encoding      string
	Name          string
	Description   string
	MapRegionName string
	MapLevelName  string
	GeoWidth      float64
	GeoHeight     float64
}

// NavMeshVertex represents a vertex in a MapTracker NavMesh.
type NavMeshVertex struct {
	ID       int
	X        float64
	Y        float64
	TierID   int
	EntityID int64
	Flags    int
}

// NavMeshEdge represents a directed or bidirectional edge in a MapTracker NavMesh.
type NavMeshEdge struct {
	ID            int
	SourceID      int
	DestinationID int
	Bidirectional bool
	Cost          float64
	Flags         int
}

// NavMeshTemporaryVertex represents a runtime-only vertex injected into a NavMesh.
type NavMeshTemporaryVertex struct {
	ID                 int
	X                  float64
	Y                  float64
	CostFactor         float64
	MaxConnectDistance float64
}

// NavMesh stores parsed MapTracker NavMesh data.
type NavMesh struct {
	Meta              NavMeshMeta
	Vertices          map[int]NavMeshVertex
	Edges             map[int]NavMeshEdge
	TemporaryVertices map[int]NavMeshTemporaryVertex
}

type navMeshConnectCandidate struct {
	id   int
	dist float64
	cost float64
}

type navMeshConnectChoice struct {
	temporaryID int
	vertexID    int
	dist        float64
	cost        float64
}

type navMeshConnectPlan struct {
	choices []navMeshConnectChoice
	cost    float64
}

const (
	navMeshFirstTemporaryID  = -1
	navMeshTemporaryIDOffset = -1
)

// LoadNavMesh loads a MapTracker NavMesh by map name.
func LoadNavMesh(mapName string) (*NavMesh, error) {
	relativePath := filepath.ToSlash(filepath.Join(NavMeshDataPath, mapName+".mtnm"))
	resolvedPath := resource.FindResource(relativePath)
	if resolvedPath == "" {
		return nil, fmt.Errorf("navmesh file not found: %s", relativePath)
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open navmesh file %s: %w", resolvedPath, err)
	}
	defer func() { _ = file.Close() }()

	return ParseNavMesh(file)
}

// ParseNavMesh parses MapTracker NavMesh text data.
func ParseNavMesh(r io.Reader) (*NavMesh, error) {
	scanner := bufio.NewScanner(r)
	currentSection := ""
	sectionIndex := 0
	expectedSections := []string{navMeshSectionMeta, navMeshSectionVertices, navMeshSectionEdges}

	metaValues := map[string]string{}
	vertices := map[int]NavMeshVertex{}
	edges := map[int]NavMeshEdge{}
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if section, ok := parseNavMeshSection(line); ok {
			if sectionIndex >= len(expectedSections) || section != expectedSections[sectionIndex] {
				return nil, fmt.Errorf("line %d: unexpected NavMesh section %q", lineNo, section)
			}
			currentSection = section
			sectionIndex++
			continue
		}

		if currentSection == "" {
			return nil, fmt.Errorf("line %d: data found before first section", lineNo)
		}

		switch currentSection {
		case navMeshSectionMeta:
			key, value, err := parseNavMeshKeyValue(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			if !isValidNavMeshMetaKey(key) {
				return nil, fmt.Errorf("line %d: unexpected Meta key %q", lineNo, key)
			}
			if _, ok := metaValues[key]; ok {
				return nil, fmt.Errorf("line %d: duplicate Meta key %q", lineNo, key)
			}
			metaValues[key] = value
		case navMeshSectionVertices:
			vertex, err := parseNavMeshVertex(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			if _, ok := vertices[vertex.ID]; ok {
				return nil, fmt.Errorf("line %d: duplicate vertex id %d", lineNo, vertex.ID)
			}
			vertices[vertex.ID] = vertex
		case navMeshSectionEdges:
			edge, err := parseNavMeshEdge(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			if _, ok := edges[edge.ID]; ok {
				return nil, fmt.Errorf("line %d: duplicate edge id %d", lineNo, edge.ID)
			}
			edges[edge.ID] = edge
		default:
			return nil, fmt.Errorf("line %d: unexpected section state %q", lineNo, currentSection)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read navmesh data: %w", err)
	}
	if sectionIndex != len(expectedSections) {
		return nil, fmt.Errorf("navmesh file is missing required sections")
	}

	meta, err := parseNavMeshMeta(metaValues)
	if err != nil {
		return nil, err
	}
	for _, edge := range edges {
		if _, ok := vertices[edge.SourceID]; !ok {
			return nil, fmt.Errorf("edge %d references missing source vertex %d", edge.ID, edge.SourceID)
		}
		if _, ok := vertices[edge.DestinationID]; !ok {
			return nil, fmt.Errorf("edge %d references missing target vertex %d", edge.ID, edge.DestinationID)
		}
	}

	return &NavMesh{Meta: meta, Vertices: vertices, Edges: edges}, nil
}

// AddTemporaryVertex injects a runtime-only vertex with a negative ID.
func (m *NavMesh) AddTemporaryVertex(x, y, costFactor, maxConnectDistance float64) (int, NavMeshTemporaryVertex) {
	if m.TemporaryVertices == nil {
		m.TemporaryVertices = map[int]NavMeshTemporaryVertex{}
	}
	id := navMeshFirstTemporaryID
	for {
		if _, ok := m.TemporaryVertices[id]; !ok {
			break
		}
		id += navMeshTemporaryIDOffset
	}
	vertex := NavMeshTemporaryVertex{ID: id, X: x, Y: y, CostFactor: costFactor, MaxConnectDistance: maxConnectDistance}
	m.TemporaryVertices[id] = vertex
	return id, vertex
}

// ClearTemporaryVertex removes all runtime-only vertices.
func (m *NavMesh) ClearTemporaryVertex() {
	m.TemporaryVertices = nil
}

// FindPath finds a path between vertices through this NavMesh.
func (m *NavMesh) FindPath(startID, targetID int) ([][2]float64, error) {
	if m == nil {
		return nil, fmt.Errorf("navmesh is nil")
	}

	points, adjacency := m.buildPathGraph()
	if _, ok := points[startID]; !ok {
		return nil, fmt.Errorf("start vertex %d not found", startID)
	}
	if _, ok := points[targetID]; !ok {
		return nil, fmt.Errorf("target vertex %d not found", targetID)
	}

	connectPlans := m.pathConnectPlans()
	if len(connectPlans) == 0 {
		return nil, fmt.Errorf("no available vertex plan to connect path")
	}

	var pathIDs []int
	for _, plan := range connectPlans {
		connectedAdjacency := m.applyConnectPlan(adjacency, plan)

		var err error
		pathIDs, err = astarPath(points, connectedAdjacency, startID, targetID)
		if err == nil {
			break
		}
	}
	if len(pathIDs) == 0 {
		return nil, fmt.Errorf("navmesh path not found")
	}

	path := make([][2]float64, 0, len(pathIDs))
	for _, id := range pathIDs {
		p := points[id]
		point := [2]float64{p.x, p.y}
		if len(path) > 0 && math.Hypot(path[len(path)-1][0]-point[0], path[len(path)-1][1]-point[1]) < 1e-6 {
			continue
		}
		path = append(path, point)
	}
	if len(path) == 0 {
		return nil, fmt.Errorf("path is empty")
	}
	return path, nil
}

// FindVertexByEntityID returns the first vertex associated with the entity ID.
func (m *NavMesh) FindVertexByEntityID(entityID int64) (NavMeshVertex, bool) {
	if m == nil {
		return NavMeshVertex{}, false
	}
	ids := make([]int, 0, len(m.Vertices))
	for id := range m.Vertices {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		vertex := m.Vertices[id]
		if vertex.EntityID == entityID {
			return vertex, true
		}
	}
	return NavMeshVertex{}, false
}

func parseNavMeshSection(line string) (string, bool) {
	match := navMeshSectionRegexp.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	return match[navMeshSectionRegexp.SubexpIndex("section")], true
}

func parseNavMeshKeyValue(line string) (string, string, error) {
	match := navMeshKeyValueRegexp.FindStringSubmatch(line)
	if match == nil {
		return "", "", fmt.Errorf("invalid key/value line")
	}
	return strings.TrimSpace(match[navMeshKeyValueRegexp.SubexpIndex("key")]), strings.TrimSpace(match[navMeshKeyValueRegexp.SubexpIndex("value")]), nil
}

func isValidNavMeshMetaKey(key string) bool {
	switch key {
	case "Version", "Encoding", "Name", "Description", "MapRegionName", "MapLevelName", "GeoWidth", "GeoHeight":
		return true
	default:
		return false
	}
}

func parseNavMeshMeta(values map[string]string) (NavMeshMeta, error) {
	required := []string{"Version", "Encoding", "Name", "Description", "MapRegionName", "MapLevelName", "GeoWidth", "GeoHeight"}
	for _, key := range required {
		if _, ok := values[key]; !ok {
			return NavMeshMeta{}, fmt.Errorf("navmesh Meta is missing key %q", key)
		}
	}

	version, err := strconv.Atoi(values["Version"])
	if err != nil {
		return NavMeshMeta{}, fmt.Errorf("invalid NavMesh version %q: %w", values["Version"], err)
	}
	if version != navMeshVersion {
		return NavMeshMeta{}, fmt.Errorf("unsupported NavMesh version: %d", version)
	}
	encoding := values["Encoding"]
	if encoding != navMeshEncoding {
		return NavMeshMeta{}, fmt.Errorf("unsupported NavMesh encoding: %q", encoding)
	}

	geoWidth, err := strconv.ParseFloat(values["GeoWidth"], 64)
	if err != nil {
		return NavMeshMeta{}, fmt.Errorf("invalid GeoWidth %q: %w", values["GeoWidth"], err)
	}
	geoHeight, err := strconv.ParseFloat(values["GeoHeight"], 64)
	if err != nil {
		return NavMeshMeta{}, fmt.Errorf("invalid GeoHeight %q: %w", values["GeoHeight"], err)
	}
	if values["Name"] == "" {
		return NavMeshMeta{}, fmt.Errorf("NavMesh Name cannot be empty")
	}
	if values["MapRegionName"] == "" {
		return NavMeshMeta{}, fmt.Errorf("NavMesh MapRegionName cannot be empty")
	}
	if values["MapLevelName"] == "" {
		return NavMeshMeta{}, fmt.Errorf("NavMesh MapLevelName cannot be empty")
	}
	if geoWidth <= 0 || geoHeight <= 0 {
		return NavMeshMeta{}, fmt.Errorf("NavMesh GeoWidth and GeoHeight must be positive")
	}

	return NavMeshMeta{
		Version:       version,
		Encoding:      encoding,
		Name:          values["Name"],
		Description:   values["Description"],
		MapRegionName: values["MapRegionName"],
		MapLevelName:  values["MapLevelName"],
		GeoWidth:      geoWidth,
		GeoHeight:     geoHeight,
	}, nil
}

func parseNavMeshVertex(line string) (NavMeshVertex, error) {
	match := navMeshVertexRegexp.FindStringSubmatch(line)
	if match == nil {
		return NavMeshVertex{}, fmt.Errorf("invalid vertex line")
	}

	id, err := strconv.Atoi(namedRegexpValue(navMeshVertexRegexp, match, "id"))
	if err != nil {
		return NavMeshVertex{}, fmt.Errorf("invalid vertex id: %w", err)
	}
	x, err := strconv.ParseFloat(namedRegexpValue(navMeshVertexRegexp, match, "x"), 64)
	if err != nil {
		return NavMeshVertex{}, fmt.Errorf("invalid vertex x: %w", err)
	}
	y, err := strconv.ParseFloat(namedRegexpValue(navMeshVertexRegexp, match, "y"), 64)
	if err != nil {
		return NavMeshVertex{}, fmt.Errorf("invalid vertex y: %w", err)
	}
	tierID, err := strconv.Atoi(namedRegexpValue(navMeshVertexRegexp, match, "t"))
	if err != nil {
		return NavMeshVertex{}, fmt.Errorf("invalid vertex tier id: %w", err)
	}
	entityID, err := strconv.ParseInt(namedRegexpValue(navMeshVertexRegexp, match, "e"), 10, 64)
	if err != nil {
		return NavMeshVertex{}, fmt.Errorf("invalid vertex entity id: %w", err)
	}
	flags, err := parseNavMeshVertexFlags(namedRegexpValue(navMeshVertexRegexp, match, "flags"))
	if err != nil {
		return NavMeshVertex{}, err
	}

	return NavMeshVertex{ID: id, X: roundNavMeshCoord(x), Y: roundNavMeshCoord(y), TierID: tierID, EntityID: entityID, Flags: flags}, nil
}

func parseNavMeshEdge(line string) (NavMeshEdge, error) {
	match := navMeshEdgeRegexp.FindStringSubmatch(line)
	if match == nil {
		return NavMeshEdge{}, fmt.Errorf("invalid edge line")
	}

	flagsText := namedRegexpValue(navMeshEdgeRegexp, match, "flags")
	if flagsText != "" {
		return NavMeshEdge{}, fmt.Errorf("unsupported edge flag(s): %q", flagsText)
	}

	id, err := strconv.Atoi(namedRegexpValue(navMeshEdgeRegexp, match, "id"))
	if err != nil {
		return NavMeshEdge{}, fmt.Errorf("invalid edge id: %w", err)
	}
	sourceID, err := strconv.Atoi(namedRegexpValue(navMeshEdgeRegexp, match, "source"))
	if err != nil {
		return NavMeshEdge{}, fmt.Errorf("invalid edge source id: %w", err)
	}
	destinationID, err := strconv.Atoi(namedRegexpValue(navMeshEdgeRegexp, match, "destination"))
	if err != nil {
		return NavMeshEdge{}, fmt.Errorf("invalid edge destination id: %w", err)
	}
	cost, err := strconv.ParseFloat(namedRegexpValue(navMeshEdgeRegexp, match, "cost"), 64)
	if err != nil {
		return NavMeshEdge{}, fmt.Errorf("invalid edge cost: %w", err)
	}

	return NavMeshEdge{
		ID:            id,
		SourceID:      sourceID,
		DestinationID: destinationID,
		Bidirectional: namedRegexpValue(navMeshEdgeRegexp, match, "bidirectional") == "1",
		Cost:          cost,
	}, nil
}

func namedRegexpValue(re *regexp.Regexp, match []string, name string) string {
	idx := re.SubexpIndex(name)
	if idx < 0 || idx >= len(match) {
		return ""
	}
	return match[idx]
}

func parseNavMeshVertexFlags(text string) (int, error) {
	flags := 0
	for _, ch := range text {
		switch ch {
		case 'T':
			flags |= NavMeshVertexFlagTeleportAnchor
		case 'H':
			flags |= NavMeshVertexFlagHidden
		case 'S':
			flags |= NavMeshVertexFlagSystem
		case 'R':
			flags |= NavMeshVertexFlagRare
		case 'C':
			flags |= NavMeshVertexFlagCollectable
		case 'D':
			flags |= NavMeshVertexFlagDigable
		default:
			return 0, fmt.Errorf("unsupported vertex flag: %q", ch)
		}
	}
	return flags, nil
}

func roundNavMeshCoord(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func (m *NavMesh) buildPathGraph() (map[int]astarPoint, map[int][]astarEdge) {
	points := map[int]astarPoint{}
	adjacency := map[int][]astarEdge{}
	for id, vertex := range m.Vertices {
		if vertex.Flags&NavMeshVertexFlagHidden != 0 {
			continue
		}
		points[id] = astarPoint{x: vertex.X, y: vertex.Y}
	}
	for id, vertex := range m.TemporaryVertices {
		points[id] = astarPoint{x: vertex.X, y: vertex.Y}
	}
	for _, edge := range m.Edges {
		if _, ok := points[edge.SourceID]; !ok {
			continue
		}
		if _, ok := points[edge.DestinationID]; !ok {
			continue
		}
		adjacency[edge.SourceID] = append(adjacency[edge.SourceID], astarEdge{to: edge.DestinationID, cost: edge.Cost})
		if edge.Bidirectional {
			adjacency[edge.DestinationID] = append(adjacency[edge.DestinationID], astarEdge{to: edge.SourceID, cost: edge.Cost})
		}
	}
	return points, adjacency
}

func (m *NavMesh) pathConnectPlans() []navMeshConnectPlan {
	temporaryIDs := sortedNavMeshTemporaryVertexIDs(m.TemporaryVertices)
	if len(temporaryIDs) == 0 {
		return nil
	}
	candidateLists := make([][]navMeshConnectCandidate, 0, len(temporaryIDs))
	for _, id := range temporaryIDs {
		candidates := m.pathConnectCandidates(m.TemporaryVertices[id])
		if len(candidates) == 0 {
			return nil
		}
		candidateLists = append(candidateLists, candidates)
	}
	plans := []navMeshConnectPlan{}
	m.appendPathConnectPlans(&plans, nil, temporaryIDs, candidateLists, 0, 0)
	sort.Slice(plans, func(i, j int) bool {
		if math.Abs(plans[i].cost-plans[j].cost) > 1e-9 {
			return plans[i].cost < plans[j].cost
		}
		return compareNavMeshConnectChoices(plans[i].choices, plans[j].choices) < 0
	})
	return plans
}

func (m *NavMesh) appendPathConnectPlans(plans *[]navMeshConnectPlan, choices []navMeshConnectChoice, temporaryIDs []int, candidateLists [][]navMeshConnectCandidate, index int, cost float64) {
	if index == len(temporaryIDs) {
		*plans = append(*plans, navMeshConnectPlan{choices: append([]navMeshConnectChoice(nil), choices...), cost: cost})
		return
	}
	temporaryID := temporaryIDs[index]
	for _, candidate := range candidateLists[index] {
		choice := navMeshConnectChoice{temporaryID: temporaryID, vertexID: candidate.id, dist: candidate.dist, cost: candidate.cost}
		m.appendPathConnectPlans(plans, append(choices, choice), temporaryIDs, candidateLists, index+1, cost+candidate.cost)
	}
}

func (m *NavMesh) pathConnectCandidates(temporaryVertex NavMeshTemporaryVertex) []navMeshConnectCandidate {
	candidates := make([]navMeshConnectCandidate, 0, len(m.Vertices))
	for id, vertex := range m.Vertices {
		if vertex.Flags&NavMeshVertexFlagHidden != 0 {
			continue
		}
		dist := math.Hypot(vertex.X-temporaryVertex.X, vertex.Y-temporaryVertex.Y)
		if dist < temporaryVertex.MaxConnectDistance {
			candidates = append(candidates, navMeshConnectCandidate{id: id, dist: dist, cost: temporaryVertex.CostFactor * dist})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].cost-candidates[j].cost) > 1e-9 {
			return candidates[i].cost < candidates[j].cost
		}
		return candidates[i].id < candidates[j].id
	})
	return candidates
}

func (m *NavMesh) applyConnectPlan(adjacency map[int][]astarEdge, plan navMeshConnectPlan) map[int][]astarEdge {
	connectedAdjacency := make(map[int][]astarEdge, len(adjacency)+2)
	for id, edges := range adjacency {
		connectedAdjacency[id] = append([]astarEdge(nil), edges...)
	}
	for _, choice := range plan.choices {
		cost := m.temporaryEdgeCost(choice.temporaryID, choice.vertexID)
		connectedAdjacency[choice.temporaryID] = append(connectedAdjacency[choice.temporaryID], astarEdge{to: choice.vertexID, cost: cost})
		connectedAdjacency[choice.vertexID] = append(connectedAdjacency[choice.vertexID], astarEdge{to: choice.temporaryID, cost: cost})
	}
	return connectedAdjacency
}

func (m *NavMesh) temporaryEdgeCost(fromID, toID int) float64 {
	fromTemporary, fromOK := m.TemporaryVertices[fromID]
	toTemporary, toOK := m.TemporaryVertices[toID]
	if fromOK && toOK {
		return math.Max(fromTemporary.CostFactor, toTemporary.CostFactor) * math.Hypot(fromTemporary.X-toTemporary.X, fromTemporary.Y-toTemporary.Y)
	}
	if fromOK {
		vertex := m.Vertices[toID]
		return fromTemporary.CostFactor * math.Hypot(fromTemporary.X-vertex.X, fromTemporary.Y-vertex.Y)
	}
	if toOK {
		vertex := m.Vertices[fromID]
		return toTemporary.CostFactor * math.Hypot(toTemporary.X-vertex.X, toTemporary.Y-vertex.Y)
	}
	return 0
}

func sortedNavMeshTemporaryVertexIDs(vertices map[int]NavMeshTemporaryVertex) []int {
	ids := make([]int, 0, len(vertices))
	for id := range vertices {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	return ids
}

func compareNavMeshConnectChoices(left, right []navMeshConnectChoice) int {
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i].vertexID != right[i].vertexID {
			return left[i].vertexID - right[i].vertexID
		}
	}
	return len(left) - len(right)
}
