package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/darraghdowney/cluso-graphdb/pkg/algorithms"
	"github.com/darraghdowney/cluso-graphdb/pkg/query"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF00FF")).
			MarginLeft(2).
			MarginTop(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFFF")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00FFFF")).
			Padding(0, 1)

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#FF00FF")).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#666666")).
				Padding(0, 2)

	contentStyle = lipgloss.NewStyle().
			MarginLeft(2).
			MarginTop(1)

	statsBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00FF00")).
			Padding(1, 2).
			MarginRight(2)

	graphBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#FFFF00")).
			Padding(1, 2)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			MarginTop(1).
			MarginLeft(2)
)

type view int

const (
	dashboardView view = iota
	nodesView
	queryView
	graphView
	metricsView
)

type keyMap struct {
	Tab      key.Binding
	ShiftTab key.Binding
	Enter    key.Binding
	Quit     key.Binding
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
}

var keys = keyMap{
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next view"),
	),
	ShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev view"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "execute"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "left"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "right"),
	),
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Enter, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab, k.ShiftTab, k.Enter},
		{k.Up, k.Down, k.Left, k.Right},
		{k.Quit},
	}
}

type model struct {
	graph       *storage.GraphStorage
	executor    *query.Executor
	currentView view
	queryInput  textinput.Model
	nodeList    list.Model
	nodeTable   table.Model
	help        help.Model
	keys        keyMap
	width       int
	height      int
	message     string
	messageErr  bool
	startTime   time.Time
	stats       storage.Statistics
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func initialModel(graph *storage.GraphStorage) model {
	ti := textinput.New()
	ti.Placeholder = "MATCH (n:Person) RETURN n"
	ti.CharLimit = 200
	ti.Width = 60

	columns := []table.Column{
		{Title: "ID", Width: 8},
		{Title: "Labels", Width: 20},
		{Title: "Properties", Width: 40},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#00FFFF")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#FF00FF")).
		Bold(false)
	t.SetStyles(s)

	return model{
		graph:       graph,
		executor:    query.NewExecutor(graph),
		currentView: dashboardView,
		queryInput:  ti,
		nodeTable:   t,
		help:        help.New(),
		keys:        keys,
		startTime:   time.Now(),
		stats:       graph.GetStatistics(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tickCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width

	case tickMsg:
		m.stats = m.graph.GetStatistics()
		return m, tickCmd()

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Tab):
			m.currentView = (m.currentView + 1) % 5
			if m.currentView == queryView {
				m.queryInput.Focus()
			} else {
				m.queryInput.Blur()
			}

		case key.Matches(msg, m.keys.ShiftTab):
			if m.currentView == 0 {
				m.currentView = 4
			} else {
				m.currentView--
			}
			if m.currentView == queryView {
				m.queryInput.Focus()
			} else {
				m.queryInput.Blur()
			}

		case key.Matches(msg, m.keys.Enter):
			if m.currentView == queryView && m.queryInput.Focused() {
				m.executeQuery()
			}
		}
	}

	// Update focused component
	switch m.currentView {
	case queryView:
		m.queryInput, cmd = m.queryInput.Update(msg)
		cmds = append(cmds, cmd)
	case nodesView:
		m.nodeTable, cmd = m.nodeTable.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) executeQuery() {
	queryStr := m.queryInput.Value()
	if queryStr == "" {
		m.message = "Query cannot be empty"
		m.messageErr = true
		return
	}

	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		m.message = fmt.Sprintf("Lexer error: %v", err)
		m.messageErr = true
		return
	}

	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		m.message = fmt.Sprintf("Parser error: %v", err)
		m.messageErr = true
		return
	}

	start := time.Now()
	results, err := m.executor.Execute(parsedQuery)
	if err != nil {
		m.message = fmt.Sprintf("Execution error: %v", err)
		m.messageErr = true
		return
	}

	elapsed := time.Since(start)
	m.message = fmt.Sprintf("Query executed successfully! Found %d rows in %s", results.Count, elapsed)
	m.messageErr = false

	// Update node table with results if they're nodes
	m.updateNodeTable(results)
}

func (m *model) updateNodeTable(results *query.ResultSet) {
	rows := make([]table.Row, 0)

	for _, row := range results.Rows {
		for _, col := range results.Columns {
			if node, ok := row[col].(*storage.Node); ok {
				labels := strings.Join(node.Labels, ", ")
				props := formatProperties(node.Properties)
				rows = append(rows, table.Row{
					fmt.Sprintf("%d", node.ID),
					labels,
					props,
				})
			}
		}
	}

	if len(rows) > 0 {
		m.nodeTable.SetRows(rows)
	}
}

func formatProperties(props map[string]storage.Value) string {
	parts := make([]string, 0)
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	if len(parts) > 3 {
		parts = parts[:3]
		parts = append(parts, "...")
	}
	return strings.Join(parts, ", ")
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var s strings.Builder

	// Title
	s.WriteString(titleStyle.Render("🔥 Cluso GraphDB - Interactive TUI"))
	s.WriteString("\n\n")

	// Tabs
	s.WriteString(m.renderTabs())
	s.WriteString("\n\n")

	// Content based on current view
	switch m.currentView {
	case dashboardView:
		s.WriteString(m.renderDashboard())
	case nodesView:
		s.WriteString(m.renderNodes())
	case queryView:
		s.WriteString(m.renderQuery())
	case graphView:
		s.WriteString(m.renderGraph())
	case metricsView:
		s.WriteString(m.renderMetrics())
	}

	// Message
	if m.message != "" {
		s.WriteString("\n\n")
		if m.messageErr {
			s.WriteString(errorStyle.Render("✗ " + m.message))
		} else {
			s.WriteString(successStyle.Render("✓ " + m.message))
		}
	}

	// Help
	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render(m.help.ShortHelpView(m.keys.ShortHelp())))

	return s.String()
}

func (m model) renderTabs() string {
	tabs := []string{"Dashboard", "Nodes", "Query", "Graph", "Metrics"}
	var renderedTabs []string

	for i, tab := range tabs {
		if view(i) == m.currentView {
			renderedTabs = append(renderedTabs, activeTabStyle.Render(tab))
		} else {
			renderedTabs = append(renderedTabs, inactiveTabStyle.Render(tab))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
}

func (m model) renderDashboard() string {
	uptime := time.Since(m.startTime).Round(time.Second)

	statsContent := fmt.Sprintf(`📊 Statistics
━━━━━━━━━━━━━━━
Nodes:     %d
Edges:     %d
Uptime:    %s
Queries:   %d

⚡ Performance
━━━━━━━━━━━━━━━
Avg Query: %.2f ms`,
		m.stats.NodeCount,
		m.stats.EdgeCount,
		uptime,
		m.stats.TotalQueries,
		m.stats.AvgQueryTime,
	)

	quickActions := `⚡ Quick Actions
━━━━━━━━━━━━━━━
[Tab]       Navigate views
[1-5]       Jump to view
[q]         Quit

🎯 Features
━━━━━━━━━━━━━━━
• Query Language (Cypher)
• Graph Algorithms
• Real-time Metrics
• Visual Graph View`

	statsBox := statsBoxStyle.Render(statsContent)
	actionsBox := statsBoxStyle.Render(quickActions)

	return contentStyle.Render(
		lipgloss.JoinHorizontal(lipgloss.Top, statsBox, actionsBox),
	)
}

func (m model) renderNodes() string {
	var s strings.Builder

	s.WriteString(headerStyle.Render("Node Browser"))
	s.WriteString("\n\n")

	s.WriteString(m.nodeTable.View())

	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render("Navigate with ↑/↓ • Press 'r' to refresh"))

	return contentStyle.Render(s.String())
}

func (m model) renderQuery() string {
	var s strings.Builder

	s.WriteString(headerStyle.Render("Query Console"))
	s.WriteString("\n\n")

	s.WriteString("Enter a Cypher-like query:\n\n")
	s.WriteString(m.queryInput.View())

	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render("Examples:\n"))
	s.WriteString(helpStyle.Render("  MATCH (n:Person) RETURN n\n"))
	s.WriteString(helpStyle.Render("  MATCH (a)-[:KNOWS]->(b) RETURN a, b\n"))
	s.WriteString(helpStyle.Render("  MATCH (n) WHERE n.age > 25 RETURN n\n"))

	return contentStyle.Render(s.String())
}

func (m model) renderGraph() string {
	var s strings.Builder

	s.WriteString(headerStyle.Render("Graph Visualization"))
	s.WriteString("\n\n")

	// Simple ASCII graph visualization
	graphViz := m.generateGraphViz()
	s.WriteString(graphBoxStyle.Render(graphViz))

	return contentStyle.Render(s.String())
}

func (m model) generateGraphViz() string {
	stats := m.stats

	if stats.NodeCount == 0 {
		return "No nodes to visualize\n\nCreate some nodes using the Query view!"
	}

	var s strings.Builder
	s.WriteString(fmt.Sprintf("Graph with %d nodes and %d edges\n\n", stats.NodeCount, stats.EdgeCount))

	// Show first few nodes with connections
	maxDisplay := 5
	if stats.NodeCount < 5 {
		maxDisplay = int(stats.NodeCount)
	}

	for i := uint64(1); i <= uint64(maxDisplay); i++ {
		node, err := m.graph.GetNode(i)
		if err != nil {
			continue
		}

		label := "Node"
		if len(node.Labels) > 0 {
			label = node.Labels[0]
		}

		name := ""
		if nameVal, ok := node.Properties["name"]; ok {
			name = fmt.Sprintf(" (%v)", nameVal)
		}

		s.WriteString(fmt.Sprintf("◉ %s %d%s\n", label, node.ID, name))

		// Show outgoing edges
		edges, err := m.graph.GetOutgoingEdges(i)
		if err == nil && len(edges) > 0 {
			for _, edge := range edges {
				if edge.ToNodeID <= uint64(maxDisplay) {
					s.WriteString(fmt.Sprintf("  └─[%s]→ Node %d\n", edge.Type, edge.ToNodeID))
				}
			}
		}
	}

	if stats.NodeCount > uint64(maxDisplay) {
		s.WriteString(fmt.Sprintf("\n... and %d more nodes\n", stats.NodeCount-uint64(maxDisplay)))
	}

	return s.String()
}

func (m model) renderMetrics() string {
	var s strings.Builder

	s.WriteString(headerStyle.Render("Performance Metrics"))
	s.WriteString("\n\n")

	// Run PageRank if we have nodes
	if m.stats.NodeCount > 0 {
		opts := algorithms.PageRankOptions{
			MaxIterations: 10,
			DampingFactor: 0.85,
			Tolerance:     1e-6,
		}

		start := time.Now()
		result, _ := algorithms.PageRank(m.graph, opts)
		elapsed := time.Since(start)

		metricsContent := fmt.Sprintf(`📈 PageRank Analysis
━━━━━━━━━━━━━━━━━━━
Iterations:  %d
Converged:   %v
Time:        %s
Nodes:       %d

Top Nodes by PageRank:`,
			result.Iterations,
			result.Converged,
			elapsed,
			len(result.Scores),
		)

		s.WriteString(statsBoxStyle.Render(metricsContent))
		s.WriteString("\n\n")

		// Show top nodes
		topNodes := getTopPageRankNodes(result.Scores, 5)
		for i, score := range topNodes {
			node, err := m.graph.GetNode(score.NodeID)
			if err != nil {
				continue
			}

			name := fmt.Sprintf("Node %d", score.NodeID)
			if nameVal, ok := node.Properties["name"]; ok {
				name = fmt.Sprintf("%v", nameVal)
			}

			bar := strings.Repeat("█", int(score.Score*50))
			s.WriteString(fmt.Sprintf("  %d. %-15s %.6f %s\n", i+1, name, score.Score, bar))
		}
	} else {
		s.WriteString(helpStyle.Render("No data available for metrics\n\nCreate some nodes and edges to see analytics!"))
	}

	return contentStyle.Render(s.String())
}

type nodeScore struct {
	NodeID uint64
	Score  float64
}

func getTopPageRankNodes(scores map[uint64]float64, limit int) []nodeScore {
	nodeScores := make([]nodeScore, 0, len(scores))
	for id, score := range scores {
		nodeScores = append(nodeScores, nodeScore{NodeID: id, Score: score})
	}

	// Simple bubble sort for top N
	for i := 0; i < len(nodeScores); i++ {
		for j := i + 1; j < len(nodeScores); j++ {
			if nodeScores[j].Score > nodeScores[i].Score {
				nodeScores[i], nodeScores[j] = nodeScores[j], nodeScores[i]
			}
		}
	}

	if len(nodeScores) > limit {
		nodeScores = nodeScores[:limit]
	}

	return nodeScores
}

func main() {
	dataDir := "./data/tui"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}

	graph, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		log.Fatalf("Failed to create graph storage: %v", err)
	}
	defer graph.Close()

	p := tea.NewProgram(initialModel(graph), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}
