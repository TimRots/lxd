//go:build linux && cgo && !agent
// +build linux,cgo,!agent

package db

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/lxc/lxd/lxd/db/query"
)

// Operation holds information about a single LXD operation running on a node
// in the cluster.
type Operation struct {
	ID          int64         // Stable database identifier
	UUID        string        // User-visible identifier
	NodeAddress string        // Address of the node the operation is running on
	Type        OperationType // Type of the operation
}

// GetLocalOperations returns all operations associated with this node.
func (c *ClusterTx) GetLocalOperations() ([]Operation, error) {
	return c.operations("node_id=?", c.nodeID)
}

// GetLocalOperationsUUIDs returns the UUIDs of all operations associated with this
// node.
func (c *ClusterTx) GetLocalOperationsUUIDs() ([]string, error) {
	stmt := "SELECT uuid FROM operations WHERE node_id=?"
	return query.SelectStrings(c.tx, stmt, c.nodeID)
}

// GetNodesWithRunningOperations returns a list of nodes that have running operations
func (c *ClusterTx) GetNodesWithRunningOperations(project string) ([]string, error) {
	stmt := `
SELECT DISTINCT nodes.address
  FROM operations
  LEFT OUTER JOIN projects ON projects.id = operations.project_id
  JOIN nodes ON nodes.id = operations.node_id
 WHERE projects.name = ? OR operations.project_id IS NULL
`
	return query.SelectStrings(c.tx, stmt, project)
}

// GetOperationsOfType returns a list operations that belong to the specified project and have the desired type.
func (c *ClusterTx) GetOperationsOfType(projectName string, opType OperationType) ([]Operation, error) {
	var ops []Operation

	stmt := `
SELECT operations.id, operations.uuid, operations.type, nodes.address
  FROM operations
  LEFT JOIN projects on projects.id = operations.project_id
  JOIN nodes on nodes.id = operations.node_id
WHERE (projects.name = ? OR operations.project_id IS NULL) and operations.type = ?
`
	rows, err := c.tx.Query(stmt, projectName, opType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var op Operation
		err := rows.Scan(&op.ID, &op.UUID, &op.Type, &op.NodeAddress)
		if err != nil {
			return nil, err
		}

		ops = append(ops, op)
	}
	if rows.Err() != nil {
		return nil, err
	}

	return ops, nil
}

// GetOperationWithID returns the operation with the given ID.
func (c *ClusterTx) GetOperationWithID(opID int) (Operation, error) {
	null := Operation{}
	operations, err := c.operations("id=?", opID)
	if err != nil {
		return null, err
	}
	switch len(operations) {
	case 0:
		return null, ErrNoSuchObject
	case 1:
		return operations[0], nil
	default:
		return null, fmt.Errorf("More than one operation matches")
	}
}

// GetOperationByUUID returns the operation with the given UUID.
func (c *ClusterTx) GetOperationByUUID(uuid string) (Operation, error) {
	null := Operation{}
	operations, err := c.operations("uuid=?", uuid)
	if err != nil {
		return null, err
	}
	switch len(operations) {
	case 0:
		return null, ErrNoSuchObject
	case 1:
		return operations[0], nil
	default:
		return null, fmt.Errorf("More than one operation matches")
	}
}

// CreateOperation adds a new operations to the table.
func (c *ClusterTx) CreateOperation(project, uuid string, typ OperationType) (int64, error) {
	var projectID interface{}

	if project != "" {
		var err error
		projectID, err = c.GetProjectID(project)
		if err != nil {
			return -1, errors.Wrap(err, "Fetch project ID")
		}
	} else {
		projectID = nil
	}

	columns := []string{"uuid", "node_id", "type", "project_id"}
	values := []interface{}{uuid, c.nodeID, typ, projectID}
	return query.UpsertObject(c.tx, "operations", columns, values)
}

// RemoveOperation removes the operation with the given UUID.
func (c *ClusterTx) RemoveOperation(uuid string) error {
	result, err := c.tx.Exec("DELETE FROM operations WHERE uuid=?", uuid)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("query deleted %d rows instead of 1", n)
	}
	return nil
}

// Remove all operations for the given node.
func (c *ClusterTx) removeNodeOperations(nodeID int64) error {
	_, err := c.tx.Exec("DELETE FROM operations WHERE node_id=?", nodeID)
	if err != nil {
		return err
	}

	return nil
}

// Operations returns all operations in the cluster, filtered by the given clause.
func (c *ClusterTx) operations(where string, args ...interface{}) ([]Operation, error) {
	operations := []Operation{}
	dest := func(i int) []interface{} {
		operations = append(operations, Operation{})
		return []interface{}{
			&operations[i].ID,
			&operations[i].UUID,
			&operations[i].NodeAddress,
			&operations[i].Type,
		}
	}
	sql := `
SELECT operations.id, uuid, nodes.address, type FROM operations JOIN nodes ON nodes.id = node_id `
	if where != "" {
		sql += fmt.Sprintf("WHERE %s ", where)
	}
	sql += "ORDER BY operations.id"
	stmt, err := c.tx.Prepare(sql)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	err = query.SelectObjects(stmt, dest, args...)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch operations")
	}
	return operations, nil
}
