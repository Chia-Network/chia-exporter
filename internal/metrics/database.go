package metrics

// initTables ensures that the tables required exist and have the correct columns present
func (m *Metrics) initTables() error {
	if m.mysqlClient == nil {
		return nil
	}
	query := "CREATE TABLE IF NOT EXISTS `asn` (" +
		"  `asn` int unsigned NOT NULL," +
		"  `organization` VARCHAR(255) NOT NULL," +
		"  `count` int unsigned NOT NULL," +
		"  `network` VARCHAR(255) NOT NULL," +
		"UNIQUE KEY `asn-unique` (`asn`)" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;"

	result, err := m.mysqlClient.Query(query)
	if err != nil {
		return err
	}
	return result.Close()
}

// DeleteASNRecords deletes all records from the asn table in the database
func (m *Metrics) DeleteASNRecords(networkName string) error {
	if m.mysqlClient == nil {
		return nil
	}
	query := "DELETE from asn where network=?;"
	result, err := m.mysqlClient.Query(query, networkName)
	if err != nil {
		return err
	}
	return result.Close()
}
