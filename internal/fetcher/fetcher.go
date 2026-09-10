package fetcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nutanix/ntnx-topo/internal/api"
	"github.com/nutanix/ntnx-topo/internal/api/prism"
	v2client "github.com/nutanix/ntnx-topo/internal/api/v2"
	v3client "github.com/nutanix/ntnx-topo/internal/api/v3"
	v4client "github.com/nutanix/ntnx-topo/internal/api/v4"
	"github.com/nutanix/ntnx-topo/internal/config"
	"github.com/nutanix/ntnx-topo/internal/model"
)

type pcSource struct {
	host     string
	username string
	password string
	v4       *v4client.Client
	v3       *v3client.Client
	prism    *prism.Client
}

type hostCred struct {
	user string
	pass string
}

// authState is what we know about one host's credentials. Credentials are
// checked once with a single call; a rejection is remembered so the poll loop
// never sends a known-bad password again. Repeating it every 5 s is how you trip
// a Prism account-lockout policy.
type authState int

const (
	// authUnknown means we have not checked yet, or the last attempt could not
	// reach the host and so said nothing about the credentials.
	authUnknown authState = iota
	// authOK means one call succeeded, so the normal API calls may run.
	authOK
	// authRejected means the host answered 401/403. Sticky until new
	// credentials arrive through SetHostLogin.
	authRejected
)

// Fetcher polls Nutanix APIs on a timer and pushes state snapshots to UpdateCh.
type Fetcher struct {
	pcs      []*pcSource
	pe       *v2client.Client
	insecure bool
	timeout  time.Duration
	ticker   time.Duration

	selMu      sync.Mutex
	selectedID string

	credMu    sync.Mutex
	hostCreds map[string]hostCred
	addresses map[string]string
	auth      map[string]authState

	// configured holds the normalized hosts the operator gave us. Addresses
	// discovered through the API are never in here, so they never receive
	// configured credentials without the user typing them first.
	configured map[string]bool

	UpdateCh  chan model.StateSnapshot
	refreshCh chan struct{}
	state     *model.AppState
}

func New(cfg *config.Config, updateCh chan model.StateSnapshot) *Fetcher {
	f := &Fetcher{
		insecure:   cfg.Insecure,
		timeout:    cfg.RequestTimeout,
		ticker:     cfg.PollInterval,
		configured: map[string]bool{},
		auth:       map[string]authState{},
		UpdateCh:   updateCh,
		refreshCh:  make(chan struct{}, 1),
		state:      model.NewAppState(),
	}

	for _, ep := range cfg.PCEndpoints() {
		f.pcs = append(f.pcs, newPCSource(ep.IP, ep.Username, ep.Password, cfg.Insecure, cfg.RequestTimeout))
		f.configured[api.NormalizeHost(ep.IP)] = true
	}

	if cfg.PrismElementIP != "" {
		f.configured[api.NormalizeHost(cfg.PrismElementIP)] = true
		user, pass := cfg.Username, cfg.Password
		if user == "" && len(cfg.PCEndpoints()) > 0 {
			user = cfg.PCEndpoints()[0].Username
			pass = cfg.PCEndpoints()[0].Password
		}
		f.pe = v2client.NewClient(cfg.PrismElementIP, user, pass, cfg.Insecure, cfg.RequestTimeout)
	}

	return f
}

func newPCSource(host, user, pass string, insecure bool, timeout time.Duration) *pcSource {
	return &pcSource{
		host:     host,
		username: user,
		password: pass,
		v4:       v4client.NewClient(host, user, pass, insecure, timeout),
		v3:       v3client.NewClient(host, user, pass, insecure, timeout),
		prism:    prism.NewClient(host, user, pass, insecure, timeout),
	}
}

// SetSelected records the inventory item the UI wants topology for.
func (f *Fetcher) SetSelected(id string) {
	f.selMu.Lock()
	changed := id != "" && id != f.selectedID
	if id != "" {
		f.selectedID = id
	}
	f.selMu.Unlock()
	if changed {
		f.ManualRefresh()
	}
}

// InsecureTLS reports whether certificate verification is disabled, so the UI
// can say so while it is running.
func (f *Fetcher) InsecureTLS() bool { return f.insecure }

func (f *Fetcher) selected() string {
	f.selMu.Lock()
	defer f.selMu.Unlock()
	return f.selectedID
}

// SetHostLogin stores a session-only address and credentials for one inventory
// entity (never written to config.yaml) and rebuilds API clients for that host.
// entityID may be empty when the caller only knows the address.
func (f *Fetcher) SetHostLogin(entityID, ip, user, pass string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	norm := api.NormalizeHost(ip)
	f.credMu.Lock()
	if f.hostCreds == nil {
		f.hostCreds = map[string]hostCred{}
	}
	f.hostCreds[norm] = hostCred{user: user, pass: pass}
	if entityID != "" {
		if f.addresses == nil {
			f.addresses = map[string]string{}
		}
		f.addresses[entityID] = ip
	}
	for i, src := range f.pcs {
		if api.NormalizeHost(src.host) == norm {
			f.pcs[i] = newPCSource(src.host, user, pass, f.insecure, f.timeout)
		}
	}
	if f.pe != nil && api.NormalizeHost(f.pe.Host()) == norm {
		f.pe = v2client.NewClient(f.pe.Host(), user, pass, f.insecure, f.timeout)
	}
	f.credMu.Unlock()
	f.ManualRefresh()
}

func (f *Fetcher) credsFor(host string) (user, pass string, ok bool) {
	f.credMu.Lock()
	defer f.credMu.Unlock()
	c, ok := f.hostCreds[api.NormalizeHost(host)]
	if !ok {
		return "", "", false
	}
	return c.user, c.pass, true
}

// trustedHost reports whether we may send credentials to host. Only two things
// earn that: the operator configured it, or the user typed a login for it in the
// overlay. Addresses that came out of an API response (an availability-zone
// registration, say) are not trusted, because a peer that can choose the address
// could otherwise collect the configured admin credentials.
func (f *Fetcher) trustedHost(host string) bool {
	norm := api.NormalizeHost(host)
	if norm == "" {
		return false
	}
	f.credMu.Lock()
	defer f.credMu.Unlock()
	if f.configured[norm] {
		return true
	}
	_, ok := f.hostCreds[norm]
	return ok
}

// trustedCreds returns the credentials we may use for host: the session login
// the user typed, else the login the operator configured for that exact host.
// It never falls back to another host's credentials.
func (f *Fetcher) trustedCreds(host string) (user, pass string, ok bool) {
	if u, p, found := f.credsFor(host); found {
		return u, p, true
	}
	norm := api.NormalizeHost(host)
	f.credMu.Lock()
	defer f.credMu.Unlock()
	if !f.configured[norm] {
		return "", "", false
	}
	for _, src := range f.pcs {
		if api.NormalizeHost(src.host) == norm {
			return src.username, src.password, true
		}
	}
	return "", "", false
}

// addressFor returns the address the user typed for an entity the API did not
// give us an address for.
func (f *Fetcher) addressFor(entityID string) string {
	if entityID == "" {
		return ""
	}
	f.credMu.Lock()
	defer f.credMu.Unlock()
	return f.addresses[entityID]
}

// IsAuthErr reports HTTP 401/403 (or equivalent) from a Nutanix API call.
func IsAuthErr(err error) bool {
	if err == nil {
		return false
	}
	reach, credsOK, _ := classifyRemoteErr(err)
	return reach && !credsOK
}

// FetchEntityConfig loads Config-pane fields for a clicked topology node.
func (f *Fetcher) FetchEntityConfig(ref model.EntityRef, inv model.Inventory, cluster model.Cluster) ([]model.ConfigField, error) {
	fallback := entityFallback(ref)
	src := f.sourceForEntity(ref, inv)
	if src == nil {
		return fallback, fmt.Errorf("no API client for this entity")
	}

	switch ref.Kind {
	case model.EntityPC, model.EntityPE:
		var fields []model.ConfigField
		var getErr error
		if src.v4 != nil {
			cl, err := src.v4.GetClusterById(ref.ID)
			if err == nil {
				fields = v4client.ClusterFields(*cl)
			} else {
				slog.Debug("v4 GetClusterById failed", "id", ref.ID, "error", err)
				getErr = err
			}
		}
		if len(fields) == 0 && src.v3 != nil {
			ent, err := src.v3.GetCluster(ref.ID)
			if err == nil {
				fields = v3client.ClusterFields(*ent)
			} else {
				getErr = err
			}
		}
		if len(fields) == 0 {
			if getErr != nil {
				return fallback, getErr
			}
			return fallback, fmt.Errorf("cluster GET failed")
		}
		if ref.Kind == model.EntityPC {
			fields = f.appendPCProducts(src, ref, fields)
		}
		return fields, nil

	case model.EntityHost, model.EntityCVM:
		hostID := ref.ID
		if strings.HasPrefix(hostID, "cvm-") {
			hostID = strings.TrimPrefix(hostID, "cvm-")
		}
		clusterID := ref.ClusterID
		if clusterID == "" {
			clusterID = cluster.ID
		}
		if src.v4 != nil {
			h, err := src.v4.GetHostById(clusterID, hostID)
			if err == nil {
				fields := v4client.HostFields(*h)
				if len(fields) > 0 {
					return fields, nil
				}
			} else {
				slog.Debug("v4 GetHostById failed", "host", hostID, "error", err)
			}
		}
		if src.v3 != nil {
			ent, err := src.v3.GetHost(hostID)
			if err == nil {
				fields := v3client.HostFields(*ent)
				if len(fields) > 0 {
					return fields, nil
				}
			} else {
				return fallback, err
			}
		}
		return fallback, fmt.Errorf("host GET failed")

	case model.EntitySwitch:
		hostNames := map[string]string{}
		for _, n := range cluster.Hosts() {
			hostNames[n.ID] = n.Name
		}
		name := ref.Switch
		if name == "" {
			name = ref.Name
		}
		if src.v4 != nil {
			nics, err := src.v4.ListHostNics()
			if err != nil {
				return fallback, err
			}
			fields := v4client.SwitchFields(name, nics, hostNames)
			if len(fields) > 0 {
				return fields, nil
			}
		}
		return fallback, nil
	}

	return fallback, nil
}

func (f *Fetcher) sourceForEntity(ref model.EntityRef, inv model.Inventory) *pcSource {
	switch ref.Kind {
	case model.EntityPC:
		if pc, ok := inv.FindPC(ref.ID); ok {
			host := pc.IP
			if host == "" {
				host = f.addressFor(pc.ID)
			}
			return f.sourceByHost(host)
		}
		if ref.IP != "" {
			return f.sourceByHost(ref.IP)
		}
	case model.EntityPE:
		if pe, parent, ok := inv.FindPE(ref.ID); ok {
			return f.sourceFor(inv, parent, pe)
		}
		if ref.IP != "" {
			return f.sourceByHost(ref.IP)
		}
	default:
		if ref.ClusterID != "" {
			if pe, parent, ok := inv.FindPE(ref.ClusterID); ok {
				return f.sourceFor(inv, parent, pe)
			}
			if pc, ok := inv.FindPC(ref.ClusterID); ok {
				return f.sourceByHost(pc.IP)
			}
		}
		if item, ok := inv.Find(ref.ClusterID); ok && item.IP != "" {
			return f.sourceByHost(item.IP)
		}
	}
	if len(f.pcs) > 0 {
		return f.pcs[0]
	}
	return nil
}

func (f *Fetcher) appendPCProducts(src *pcSource, ref model.EntityRef, fields []model.ConfigField) []model.ConfigField {
	if src == nil || src.prism == nil {
		return fields
	}
	dmID := ""
	if src.v3 != nil {
		azs, err := src.v3.ListAvailabilityZones()
		if err != nil {
			slog.Debug("AZ list for products failed", "host", src.host, "error", err)
		} else {
			dmID = localDomainManagerID(azs)
		}
	}
	if dmID == "" && api.LooksLikeUUID(ref.ID) {
		dmID = ref.ID
	}
	if dmID == "" {
		fields = append(fields, model.ConfigField{Key: "error", Value: "no domain manager id for products"})
		return fields
	}
	products, err := src.prism.ListProducts(dmID)
	if err != nil {
		slog.Debug("ListProducts failed", "dm", dmID, "error", err)
		fields = append(fields, model.ConfigField{Key: "error", Value: "products: " + err.Error()})
		return fields
	}
	names := prism.EnabledProductNames(products)
	if len(names) > 0 {
		fields = append(fields, model.ConfigField{Key: "products", Value: strings.Join(names, ", ")})
	} else {
		fields = append(fields, model.ConfigField{Key: "products", Value: "none enabled"})
	}
	return fields
}

func entityFallback(ref model.EntityRef) []model.ConfigField {
	var out []model.ConfigField
	add := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		out = append(out, model.ConfigField{Key: k, Value: v})
	}
	add("kind", string(ref.Kind))
	add("name", ref.Name)
	add("extId", ref.ID)
	add("IP", ref.IP)
	add("switch", ref.Switch)
	return out
}

// ManualRefresh triggers an immediate fetch cycle (non-blocking).
func (f *Fetcher) ManualRefresh() {
	select {
	case f.refreshCh <- struct{}{}:
	default:
	}
}

// Start runs the polling loop. It blocks until ctx is cancelled.
func (f *Fetcher) Start(ctx context.Context) {
	f.fetchAndPublish()

	tick := time.NewTicker(f.ticker)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			f.fetchAndPublish()
		case <-f.refreshCh:
			f.fetchAndPublish()
		}
	}
}

func (f *Fetcher) fetchAndPublish() {
	f.state.SetActiveFetch(true)
	f.publish()

	start := time.Now()
	inv, topo, apiVersion, errs := f.fetchAll()
	latency := time.Since(start)

	f.state.SetActiveFetch(false)
	f.state.SetInventory(inv)
	if topo != nil {
		f.state.SetCluster(*topo)
	}
	f.state.SetMeta(apiVersion, latency, errs)
	f.publish()
}

func (f *Fetcher) fetchAll() (model.Inventory, *model.Cluster, string, []string) {
	var errs []string
	inv := model.Inventory{}
	apiVersion := "none"
	seen := map[string]bool{}

	for _, src := range f.pcs {
		pc, ver, err := f.fetchPCInventory(src)
		if err != nil {
			slog.Warn("inventory failed", "host", src.host, "error", err)
			errs = append(errs, src.host+": "+err.Error())
			reach, creds, msg := classifyRemoteErr(err)
			pc = model.PCNode{
				InventoryItem: model.InventoryItem{
					ID:        "pc-" + src.host,
					Name:      src.host,
					IP:        src.host,
					Status:    model.StatusUnknown,
					Kind:      model.KindPC,
					Reachable: reach,
					CredsOK:   creds,
					Message:   msg,
				},
			}
		} else if ver != "" && apiVersion == "none" {
			apiVersion = ver
		}
		key := api.NormalizeHost(src.host)
		if seen[key] {
			continue
		}
		seen[key] = true

		azs, azErr := src.v3.ListAvailabilityZones()
		if azErr != nil {
			slog.Debug("availability zones unavailable", "host", src.host, "error", azErr)
		} else if name := localAZDisplayName(azs); name != "" && (pc.Name == "" || pc.Name == src.host || pc.Name == "Unnamed") {
			pc.Name = name
		}
		inv.PCs = append(inv.PCs, pc)

		if azErr != nil {
			continue
		}
		regs := f.remotePCRegistrations(src, azs)
		for _, az := range azs {
			if strings.EqualFold(az.PlaneType, "Local") {
				continue
			}
			reg, ip := matchRemotePC(az, regs)
			if ip == "" {
				ip = az.Host
			}
			if ip == "" || api.LooksLikeUUID(ip) {
				ip = f.addressFor(azNodeID(az))
			}
			if ip == "" || api.LooksLikeUUID(ip) {
				slog.Debug("remote AZ has no address", "az", az.Name, "id", az.ID)
				placeholder := remoteAZPlaceholder(az, "", reg)
				inv.Pairings = append(inv.Pairings, model.PCPairing{FromID: pc.ID, ToID: placeholder.ID})
				inv.PCs = append(inv.PCs, placeholder)
				continue
			}
			norm := api.NormalizeHost(ip)
			if seen[norm] {
				if id := pcIDByHost(inv, ip); id != "" && id != pc.ID {
					inv.Pairings = append(inv.Pairings, model.PCPairing{FromID: pc.ID, ToID: id})
				}
				continue
			}
			seen[norm] = true
			if !f.trustedHost(ip) {
				// The address came from the API, so it does not get this PC's
				// credentials. Keep it visible and clickable instead.
				slog.Debug("remote AZ address not trusted, waiting for a login",
					"az", az.Name, "id", az.ID, "ip", ip)
				placeholder := remoteAZPlaceholder(az, ip, reg)
				inv.Pairings = append(inv.Pairings, model.PCPairing{FromID: pc.ID, ToID: placeholder.ID})
				inv.PCs = append(inv.PCs, placeholder)
				continue
			}
			user, pass, ok := f.trustedCreds(ip)
			if !ok {
				slog.Warn("trusted AZ address has no credentials", "az", az.Name, "ip", ip)
				placeholder := remoteAZPlaceholder(az, ip, reg)
				inv.Pairings = append(inv.Pairings, model.PCPairing{FromID: pc.ID, ToID: placeholder.ID})
				inv.PCs = append(inv.PCs, placeholder)
				continue
			}
			remote := f.discoverRemoteAZ(az, ip, user, pass, reg, &errs)
			inv.Pairings = append(inv.Pairings, model.PCPairing{FromID: pc.ID, ToID: remote.ID})
			inv.PCs = append(inv.PCs, remote)
		}
	}

	if f.pe != nil {
		topo, err := f.pe.FetchTopology()
		if err != nil {
			slog.Warn("v2 PE inventory failed", "error", err)
			errs = append(errs, "pe: "+err.Error())
			reach, creds, msg := classifyRemoteErr(err)
			inv.PEs = append(inv.PEs, model.PENode{
				InventoryItem: model.InventoryItem{
					ID:        "pe-" + f.pe.Host(),
					Name:      f.pe.Host(),
					IP:        f.pe.Host(),
					Status:    model.StatusUnknown,
					Kind:      model.KindPE,
					Reachable: reach,
					CredsOK:   creds,
					Message:   msg,
				},
			})
		} else {
			if apiVersion == "none" {
				apiVersion = "v2"
			}
			inv.PEs = append(inv.PEs, model.PENode{
				InventoryItem: model.InventoryItem{
					ID:        topo.ID,
					Name:      topo.Name,
					IP:        f.pe.Host(),
					Status:    model.StatusUp,
					Kind:      model.KindPE,
					Reachable: true,
					CredsOK:   true,
				},
			})
		}
	}

	selected := f.selected()
	if selected == "" || !inv.HasItem(selected) {
		selected = inv.FirstSelectable()
		if selected != "" {
			f.selMu.Lock()
			f.selectedID = selected
			f.selMu.Unlock()
		}
	}

	topo := f.fetchSelectedTopology(inv, selected, &errs, &apiVersion)
	return inv, topo, apiVersion, errs
}

func (f *Fetcher) fetchPCInventory(src *pcSource) (model.PCNode, string, error) {
	if src.v4 != nil {
		pc, err := src.v4.ListInventory()
		if err == nil {
			if pc.IP == "" {
				pc.IP = src.host
			}
			return *pc, "v4", nil
		}
		slog.Warn("v4 inventory failed, falling back to v3", "host", src.host, "error", err)
	}
	if src.v3 != nil {
		pc, err := src.v3.ListInventory()
		if err == nil {
			if pc.IP == "" {
				pc.IP = src.host
			}
			return *pc, "v3", nil
		}
		return model.PCNode{}, "", err
	}
	return model.PCNode{}, "", nil
}

// discoverRemoteAZ queries a paired PC. The credentials are passed in rather
// than taken from the discovering PC: the caller has already established that
// this address is one we may authenticate to, and with which login.
func (f *Fetcher) discoverRemoteAZ(az v3client.AvailabilityZone, ip, user, pass string, reg prism.Registration, errs *[]string) model.PCNode {
	name := az.DisplayName
	if name == "" {
		name = az.Name
	}
	if name == "" {
		name = ip
	}
	item := model.InventoryItem{
		ID:        azNodeID(az),
		Name:      name,
		IP:        ip,
		Status:    model.StatusUnknown,
		Kind:      model.KindPC,
		Reachable: false,
		CredsOK:   false,
	}

	remote := newPCSource(ip, user, pass, f.insecure, f.timeout)
	pc, _, err := f.fetchPCInventory(remote)
	if err != nil {
		reach, creds, msg := classifyRemoteErr(err)
		item.Reachable = reach
		item.CredsOK = creds
		item.Message = msg
		if !reach {
			item.Status = model.StatusDown
		}
		*errs = append(*errs, ip+": "+err.Error())
		slog.Warn("remote AZ inventory failed", "az", name, "ip", ip, "regStatus", reg.ConnectivityStatus, "regType", reg.ClusterType, "error", err)
		return model.PCNode{InventoryItem: item}
	}
	pc.IP = ip
	if pc.Name == "" || pc.Name == ip || pc.Name == "Unnamed" {
		pc.Name = name
	}
	if pc.ID == "" {
		pc.ID = item.ID
	}
	return pc
}

func (f *Fetcher) remotePCRegistrations(src *pcSource, azs []v3client.AvailabilityZone) map[string]prism.Registration {
	dmID := localDomainManagerID(azs)
	if dmID == "" || src.prism == nil {
		return nil
	}
	regs, err := src.prism.ListRegistrations(dmID)
	if err != nil {
		slog.Warn("domain-manager registrations failed", "host", src.host, "error", err)
		return nil
	}
	return prism.IndexByExtID(regs)
}

func localDomainManagerID(azs []v3client.AvailabilityZone) string {
	for _, az := range azs {
		if strings.EqualFold(az.PlaneType, "Local") && api.LooksLikeUUID(az.ManagementURL) {
			return az.ManagementURL
		}
	}
	return ""
}

func localAZDisplayName(azs []v3client.AvailabilityZone) string {
	for _, az := range azs {
		if strings.EqualFold(az.PlaneType, "Local") && az.DisplayName != "" {
			return az.DisplayName
		}
	}
	return ""
}

func matchRemotePC(az v3client.AvailabilityZone, regs map[string]prism.Registration) (prism.Registration, string) {
	for _, key := range []string{az.ID, az.ManagementURL} {
		if key == "" || regs == nil {
			continue
		}
		reg, ok := regs[strings.ToLower(key)]
		if !ok {
			continue
		}
		return reg, reg.IP
	}
	return prism.Registration{}, ""
}

func classifyRemoteErr(err error) (reachable, credsOK bool, msg string) {
	if err == nil {
		return true, true, ""
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 401 || apiErr.StatusCode == 403 {
			return true, false, "creds required"
		}
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "401") || strings.Contains(s, "unauthorized") || strings.Contains(s, "403") || strings.Contains(s, "forbidden") {
		return true, false, "creds required"
	}
	return false, false, "unreachable"
}

func pcIDByHost(inv model.Inventory, host string) string {
	norm := api.NormalizeHost(host)
	for _, pc := range inv.PCs {
		if api.NormalizeHost(pc.IP) == norm || api.NormalizeHost(pc.ID) == norm {
			return pc.ID
		}
	}
	return ""
}

// remoteAZPlaceholder describes an AZ we could not query because no address was
// discovered. Health comes from what the AZ list and registration reported, not
// from a probe we were never able to run.
func remoteAZPlaceholder(az v3client.AvailabilityZone, ip string, reg prism.Registration) model.PCNode {
	name := az.DisplayName
	if name == "" {
		name = az.Name
	}
	item := model.InventoryItem{
		ID:        azNodeID(az),
		Name:      name,
		IP:        ip,
		Kind:      model.KindPC,
		Status:    model.StatusUp,
		Reachable: true,
		CredsOK:   false,
		Message:   "login required",
	}
	if azReportedDown(az, reg) {
		item.Status = model.StatusDown
		item.Reachable = false
		item.Message = "unreachable"
	}
	return model.PCNode{InventoryItem: item}
}

func azNodeID(az v3client.AvailabilityZone) string {
	if az.ID != "" {
		return az.ID
	}
	name := az.DisplayName
	if name == "" {
		name = az.Name
	}
	return "az-" + name
}

// azReportedDown is true only when the API explicitly says the pairing is down.
// Empty or unknown status stays healthy so an AZ we simply cannot reach from
// this laptop is not painted red.
func azReportedDown(az v3client.AvailabilityZone, reg prism.Registration) bool {
	for _, s := range []string{reg.ConnectivityStatus, az.State} {
		s = strings.ToUpper(strings.TrimSpace(s))
		if strings.Contains(s, "DISCONNECT") || strings.Contains(s, "UNREACHABLE") || strings.Contains(s, "ERROR") {
			return true
		}
	}
	return false
}

func (f *Fetcher) fetchSelectedTopology(inv model.Inventory, selected string, errs *[]string, apiVersion *string) *model.Cluster {
	if selected == "" {
		return unknownTopology()
	}

	if pc, ok := inv.FindPC(selected); ok {
		if !pc.Reachable || !pc.CredsOK {
			return v4client.PCOverview(pc)
		}
		return v4client.PCOverview(pc)
	}

	pe, parentID, ok := inv.FindPE(selected)
	if !ok {
		return unknownTopology()
	}

	if f.pe != nil && (pe.ID == "pe-"+f.pe.Host() || pe.IP == f.pe.Host() || pe.Name == f.pe.Host()) {
		topo, err := f.pe.FetchTopology()
		if err != nil {
			*errs = append(*errs, "v2: "+err.Error())
			return unknownTopology()
		}
		if *apiVersion == "none" {
			*apiVersion = "v2"
		}
		return topo
	}

	src := f.sourceFor(inv, parentID, pe)
	if src == nil && f.pe != nil {
		topo, err := f.pe.FetchTopology()
		if err == nil {
			return topo
		}
		*errs = append(*errs, "v2: "+err.Error())
		return unknownTopology()
	}
	if src == nil {
		return unknownTopology()
	}

	if src.v4 != nil {
		topo, err := src.v4.FetchPETopology(pe.ID)
		if err == nil {
			if *apiVersion == "none" {
				*apiVersion = "v4"
			}
			return topo
		}
		slog.Warn("v4 PE topology failed, trying v3", "cluster", pe.ID, "error", err)
		*errs = append(*errs, "v4: "+err.Error())
	}
	if src.v3 != nil {
		topo, err := src.v3.FetchPETopology(pe.ID)
		if err == nil {
			if *apiVersion == "none" {
				*apiVersion = "v3"
			}
			return topo
		}
		*errs = append(*errs, "v3: "+err.Error())
	}
	return unknownTopology()
}

func (f *Fetcher) sourceFor(inv model.Inventory, parentID string, pe model.PENode) *pcSource {
	host := ""
	if parentID != "" {
		if pc, ok := inv.FindPC(parentID); ok {
			host = pc.IP
		}
	}
	if host == "" {
		host = pe.IP
	}
	return f.sourceByHost(host)
}

func (f *Fetcher) sourceByHost(host string) *pcSource {
	if host == "" {
		return nil
	}
	if user, pass, ok := f.credsFor(host); ok {
		return newPCSource(host, user, pass, f.insecure, f.timeout)
	}
	norm := api.NormalizeHost(host)
	f.credMu.Lock()
	pcs := f.pcs
	f.credMu.Unlock()
	for _, src := range pcs {
		if api.NormalizeHost(src.host) == norm {
			return src
		}
	}
	// No login of our own for this host. Borrowing the first PC's credentials
	// here would send them to whatever address the inventory happened to carry,
	// so the caller gets nothing and the user is asked to log in instead.
	slog.Debug("no credentials for host, refusing to guess", "host", host)
	return nil
}

func unknownTopology() *model.Cluster {
	return &model.Cluster{
		ID:    "unknown",
		Name:  "unknown",
		Kind:  model.KindPE,
		HasPC: false,
		Nodes: []model.Node{
			{ID: "switch-null", Name: model.NullSwitch, Status: model.StatusUnknown, Role: model.RoleTOR, Switch: model.NullSwitch},
		},
	}
}

func (f *Fetcher) publish() {
	snap := f.state.Snapshot()
	select {
	case f.UpdateCh <- snap:
	default:
		select {
		case <-f.UpdateCh:
		default:
		}
		select {
		case f.UpdateCh <- snap:
		default:
		}
	}
}
