package nav

import "math"

// Dijkstra uses a zero heuristic, keeping penalties admissible. All costs are
// nonnegative and infeasible edges are removed before expansion.
func Plan(m Map, states map[string]string, mission Mission) Route {
	r := Route{MissionID: mission.ID, Nodes: []string{}, Edges: []string{}, Status: "no_route", Simulated: true}
	var v VehicleProfile
	for _, p := range m.Profiles {
		if p.ID == mission.ProfileID {
			v = p
		}
	}
	dist := map[string]float64{}
	visited := map[string]bool{}
	prev := map[string]string{}
	prevEdge := map[string]Edge{}
	for _, n := range m.Nodes {
		dist[n.ID] = math.Inf(1)
	}
	if _, ok := dist[mission.Start]; !ok {
		return r
	}
	if _, ok := dist[mission.Goal]; !ok || v.ID == "" {
		return r
	}
	dist[mission.Start] = 0
	for {
		current := ""
		best := math.Inf(1)
		for _, n := range m.Nodes {
			if !visited[n.ID] && dist[n.ID] < best {
				current = n.ID
				best = dist[n.ID]
			}
		}
		if current == "" {
			return r
		}
		if current == mission.Goal {
			break
		}
		visited[current] = true
		for _, e := range m.Edges {
			next := ""
			if e.From == current {
				next = e.To
			} else if e.Bidirectional && e.To == current {
				next = e.From
			}
			if next == "" {
				continue
			}
			state := states[e.ID]
			if state == "" {
				state = "UNKNOWN"
			}
			if e.Width < v.Width+2*v.Clearance || e.TurnPenalty > v.MaxTurnPenalty || state == "BLOCKED" || (state == "UNKNOWN" && m.Policy.StrictUnknown) {
				continue
			}
			cost := e.Length + e.TurnPenalty
			switch state {
			case "UNKNOWN":
				cost += m.Policy.UnknownPenalty
			case "SUSPECTED_BLOCKED", "CLEARING":
				cost += m.Policy.RiskPenalty
			}
			if best+cost < dist[next] {
				dist[next] = best + cost
				prev[next] = current
				prevEdge[next] = e
			}
		}
	}
	r.Status = "ready"
	r.Cost = dist[mission.Goal]
	n := mission.Goal
	r.Nodes = []string{n}
	for n != mission.Start {
		e := prevEdge[n]
		r.Edges = append([]string{e.ID}, r.Edges...)
		r.Distance += e.Length
		r.ETA += e.Length / math.Min(e.SpeedLimit, v.SpeedCap)
		n = prev[n]
		r.Nodes = append([]string{n}, r.Nodes...)
	}
	return r
}
