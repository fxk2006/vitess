/*
Copyright 2020 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package endtoend

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
	"vitess.io/vitess/go/vt/vttablet/endtoend/framework"
)

//TODO: Add Counter checks in all the tests.

func TestMultipleReserveHaveDifferentConnection(t *testing.T) {
	client1 := framework.NewClient()
	client2 := framework.NewClient()

	//vstart := framework.DebugVars()

	query := "select connection_id()"

	qrc1_1, err := client1.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	defer client1.Release()
	qrc2_1, err := client2.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	defer client2.Release()
	require.NotEqual(t, qrc1_1.Rows, qrc2_1.Rows)

	qrc1_2, err := client1.Execute(query, nil)
	require.NoError(t, err)
	qrc2_2, err := client2.Execute(query, nil)
	require.NoError(t, err)
	require.Equal(t, qrc1_1.Rows, qrc1_2.Rows)
	require.Equal(t, qrc2_1.Rows, qrc2_2.Rows)

}

func TestReserveBeginRelease(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qr1, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	qr2, err := client.BeginExecute(query, nil)
	require.NoError(t, err)
	assert.Equal(t, qr1.Rows, qr2.Rows)
	assert.Equal(t, client.ReservedID(), client.TransactionID())

	require.NoError(t, client.Release())
}

func TestBeginReserveRelease(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qr1, err := client.BeginExecute(query, nil)
	require.NoError(t, err)
	defer client.Release()

	qr2, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, qr1.Rows, qr2.Rows)
	assert.Equal(t, client.ReservedID(), client.TransactionID())

	require.NoError(t, client.Release())
}

func TestReserveBeginExecute(t *testing.T) {
	client1 := framework.NewClient()
	client2 := framework.NewClient()

	query := "select connection_id()"

	qrc1_1, err := client1.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer func() {
		if client1.ReservedID() != 0 {
			t.Error("should not be reserved after release")
			_ = client1.Release()
		}
	}()
	qrc2_1, err := client2.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer func() {
		if client2.ReservedID() != 0 {
			t.Error("should not be reserved after release")
			_ = client2.Release()
		}
	}()
	require.NotEqual(t, qrc1_1.Rows, qrc2_1.Rows)
	assert.Equal(t, client1.ReservedID(), client1.TransactionID())
	assert.Equal(t, client2.ReservedID(), client2.TransactionID())

	// rows with values 1, 2 and 3 already exist
	query1 := "insert into vitess_test (intval, floatval, charval, binval) values (4, null, null, null)"
	qrc1_2, err := client1.Execute(query1, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), qrc1_2.RowsAffected, "insert should create 1 row")

	query2 := "insert into vitess_test (intval, floatval, charval, binval) values (5, null, null, null)"
	qrc2_2, err := client2.Execute(query2, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), qrc2_2.RowsAffected, "insert should create 1 row")

	query = "select intval from vitess_test"
	qrc1_2, err = client1.Execute(query, nil)
	require.NoError(t, err)
	// client1 does not see row inserted by client2
	expectedRows1 := "[[INT32(1)] [INT32(2)] [INT32(3)] [INT32(4)]]"
	assert.Equal(t, expectedRows1, fmt.Sprintf("%v", qrc1_2.Rows), "wrong result from select1")

	qrc2_2, err = client2.Execute(query, nil)
	require.NoError(t, err)
	expectedRows2 := "[[INT32(1)] [INT32(2)] [INT32(3)] [INT32(5)]]"
	assert.Equal(t, expectedRows2, fmt.Sprintf("%v", qrc2_2.Rows), "wrong result from select2")

	// Release connections without committing
	err = client1.Release()
	require.NoError(t, err)
	err = client1.Release()
	require.Error(t, err)
	err = client2.Release()
	require.NoError(t, err)
	err = client2.Release()
	require.Error(t, err)

	// test that inserts were rolled back
	client3 := framework.NewClient()
	qrc3, err := client3.Execute(query, nil)
	require.NoError(t, err)
	expectedRows := "[[INT32(1)] [INT32(2)] [INT32(3)]]"
	assert.Equal(t, expectedRows, fmt.Sprintf("%v", qrc3.Rows), "wrong result from select after release")
}

func TestCommitOnReserveConn(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qr1, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	oldRID := client.ReservedID()
	err = client.Commit()
	require.NoError(t, err)
	assert.NotEqual(t, client.ReservedID(), oldRID, "reservedID must change after commit")
	assert.EqualValues(t, 0, client.TransactionID(), "transactionID should be 0 after commit")

	qr2, err := client.Execute(query, nil)
	require.NoError(t, err)
	assert.Equal(t, qr1.Rows, qr2.Rows)
}

func TestRollbackOnReserveConn(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qr1, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	oldRID := client.ReservedID()
	err = client.Rollback()
	require.NoError(t, err)
	assert.NotEqual(t, client.ReservedID(), oldRID, "reservedID must change after rollback")
	assert.EqualValues(t, 0, client.TransactionID(), "transactionID should be 0 after commit")

	qr2, err := client.Execute(query, nil)
	require.NoError(t, err)
	assert.Equal(t, qr1.Rows, qr2.Rows)
}

func TestReserveBeginRollbackAndBeginCommitAgain(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qr1, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	oldRID := client.ReservedID()
	err = client.Rollback()
	require.NoError(t, err)
	assert.EqualValues(t, 0, client.TransactionID(), "transactionID should be 0 after rollback")
	assert.NotEqual(t, client.ReservedID(), oldRID, "reservedID must change after rollback")

	oldRID = client.ReservedID()

	qr2, err := client.BeginExecute(query, nil)
	require.NoError(t, err)

	err = client.Commit()
	require.NoError(t, err)
	assert.EqualValues(t, 0, client.TransactionID(), "transactionID should be 0 after commit")
	assert.NotEqual(t, client.ReservedID(), oldRID, "reservedID must change after rollback")

	qr3, err := client.Execute(query, nil)
	require.NoError(t, err)
	assert.Equal(t, qr1.Rows, qr2.Rows)
	assert.Equal(t, qr2.Rows, qr3.Rows)

	require.NoError(t,
		client.Release())
}

func TestReserveBeginCommitFailToReuseTxID(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	_, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	oldTxID := client.TransactionID()

	err = client.Commit()
	require.NoError(t, err)

	client.SetTransactionID(oldTxID)

	_, err = client.Execute(query, nil)
	require.Error(t, err)
	require.NoError(t,
		client.Release())
}

func TestReserveBeginRollbackFailToReuseTxID(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	_, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	oldTxID := client.TransactionID()

	err = client.Rollback()
	require.NoError(t, err)

	client.SetTransactionID(oldTxID)

	_, err = client.Execute(query, nil)
	require.Error(t, err)
	require.NoError(t,
		client.Release())
}

func TestReserveBeginCommitFailToReuseOldReservedID(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	_, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)

	oldRID := client.ReservedID()

	err = client.Commit()
	require.NoError(t, err)
	newRID := client.ReservedID()

	client.SetReservedID(oldRID)

	_, err = client.Execute(query, nil)
	require.Error(t, err)

	client.SetReservedID(newRID)
	require.NoError(t,
		client.Release())
}

func TestReserveBeginRollbackFailToReuseOldReservedID(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	_, err := client.ReserveBeginExecute(query, nil, nil)
	require.NoError(t, err)

	oldRID := client.ReservedID()

	err = client.Rollback()
	require.NoError(t, err)
	newRID := client.ReservedID()

	client.SetReservedID(oldRID)
	_, err = client.Execute(query, nil)
	require.Error(t, err)

	client.SetReservedID(newRID)
	require.NoError(t,
		client.Release())
}

func TestReserveReleaseAndFailToUseReservedIDAgain(t *testing.T) {
	client := framework.NewClient()

	query := "select 42"

	_, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)

	rID := client.ReservedID()
	require.NoError(t,
		client.Release())

	client.SetReservedID(rID)

	_, err = client.Execute(query, nil)
	require.Error(t, err)
}

func TestReserveAndFailToRunTwiceConcurrently(t *testing.T) {
	client := framework.NewClient()

	query := "select 42"

	_, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	defer client.Release()

	// WaitGroup will make defer call to wait for go func to complete.
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		_, err = client.Execute("select sleep(1)", nil)
		wg.Done()
	}()
	_, err2 := client.Execute("select sleep(1)", nil)
	wg.Wait()

	if err == nil && err2 == nil {
		assert.Fail(t, "at least one execution should fail")
	}
}

func TestBeginReserveCommitAndNewTransactionsOnSameReservedID(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qrTx, err := client.BeginExecute(query, nil)
	require.NoError(t, err)

	qrRID, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	require.Equal(t, qrTx.Rows, qrRID.Rows)

	err = client.Commit()
	require.NoError(t, err)

	qrTx, err = client.BeginExecute(query, nil)
	require.NoError(t, err)
	require.Equal(t, qrTx.Rows, qrRID.Rows)

	err = client.Commit()
	require.NoError(t, err)

	qrTx, err = client.BeginExecute(query, nil)
	require.NoError(t, err)
	require.Equal(t, qrTx.Rows, qrRID.Rows)

	err = client.Rollback()
	require.NoError(t, err)

	require.NoError(t,
		client.Release())
}

func TestBeginReserveRollbackAndNewTransactionsOnSameReservedID(t *testing.T) {
	client := framework.NewClient()

	query := "select connection_id()"

	qrTx, err := client.BeginExecute(query, nil)
	require.NoError(t, err)

	qrRID, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)
	require.Equal(t, qrTx.Rows, qrRID.Rows)

	err = client.Rollback()
	require.NoError(t, err)

	qrTx, err = client.BeginExecute(query, nil)
	require.NoError(t, err)
	require.Equal(t, qrTx.Rows, qrRID.Rows)

	err = client.Commit()
	require.NoError(t, err)

	qrTx, err = client.BeginExecute(query, nil)
	require.NoError(t, err)
	require.Equal(t, qrTx.Rows, qrRID.Rows)

	err = client.Rollback()
	require.NoError(t, err)

	require.NoError(t,
		client.Release())
}

func TestBeginReserveReleaseAndFailToUseReservedIDAndTxIDAgain(t *testing.T) {
	client := framework.NewClient()

	query := "select 42"

	_, err := client.BeginExecute(query, nil)
	require.NoError(t, err)

	_, err = client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)

	rID := client.ReservedID()
	txID := client.TransactionID()

	require.NoError(t,
		client.Release())

	client.SetReservedID(rID)
	_, err = client.Execute(query, nil)
	require.Error(t, err)

	client.SetReservedID(0)
	client.SetTransactionID(txID)
	_, err = client.Execute(query, nil)
	require.Error(t, err)
}

func TestReserveBeginReleaseAndFailToUseReservedIDAndTxIDAgain(t *testing.T) {
	client := framework.NewClient()

	query := "select 42"

	_, err := client.ReserveExecute(query, nil, nil)
	require.NoError(t, err)

	_, err = client.BeginExecute(query, nil)
	require.NoError(t, err)

	rID := client.ReservedID()
	txID := client.TransactionID()

	require.NoError(t,
		client.Release())

	client.SetReservedID(rID)
	_, err = client.Execute(query, nil)
	require.Error(t, err)

	client.SetReservedID(0)
	client.SetTransactionID(txID)
	_, err = client.Execute(query, nil)
	require.Error(t, err)
}
