package store

type ProxyPool struct {
	ID       string
	Name     string
	Type     string
	Host     string
	Username string
	Password string
	Port     int
	IsActive bool
}

func (s *Store) ListProxyPools() ([]ProxyPool, error) {
	rows, err := s.db.Query(`SELECT id, name, type, host, port, username, password, is_active FROM proxy_pools`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var out []ProxyPool

	for rows.Next() {
		var p ProxyPool

		var active int
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Host, &p.Port, &p.Username, &p.Password, &active); err != nil {
			return nil, err
		}

		p.IsActive = active != 0
		out = append(out, p)
	}

	return out, rows.Err()
}

func (s *Store) CreateProxyPool(name, typ, host string, port int, username, password string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO proxy_pools (id,name,type,host,port,username,password) VALUES (?,?,?,?,?,?,?)`,
		id, name, typ, host, port, username, password,
	)

	return id, err
}

func (s *Store) UpdateProxyPool(id, name, typ, host string, port int, username, password string, isActive bool) error {
	_, err := s.db.Exec(
		`UPDATE proxy_pools SET name=?,type=?,host=?,port=?,username=?,password=?,is_active=? WHERE id=?`,
		name, typ, host, port, username, password, boolToInt(isActive), id,
	)

	return err
}

func (s *Store) DeleteProxyPool(id string) error {
	_, err := s.db.Exec(`DELETE FROM proxy_pools WHERE id=?`, id)
	return err
}

func (s *Store) GetProxyPool(id string) (*ProxyPool, error) {
	var p ProxyPool

	var active int

	err := s.db.QueryRow(
		`SELECT id, name, type, host, port, username, password, is_active FROM proxy_pools WHERE id=?`, id,
	).Scan(&p.ID, &p.Name, &p.Type, &p.Host, &p.Port, &p.Username, &p.Password, &active)
	if err != nil {
		return nil, err
	}

	p.IsActive = active != 0

	return &p, nil
}

// UpdateProxyPoolTestStatus updates test fields without rewriting credentials.
func (s *Store) UpdateProxyPoolTestStatus(id string, isActive bool) error {
	_, err := s.db.Exec(`UPDATE proxy_pools SET is_active=? WHERE id=?`, boolToInt(isActive), id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
