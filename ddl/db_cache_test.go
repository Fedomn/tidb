// Copyright 2021 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ddl_test

import (
	"testing"
	"time"

	"github.com/pingcap/tidb/ddl"
	"github.com/pingcap/tidb/domain"
	"github.com/pingcap/tidb/errno"
	"github.com/pingcap/tidb/parser/model"
	"github.com/pingcap/tidb/parser/terror"
	"github.com/pingcap/tidb/session"
	"github.com/pingcap/tidb/sessionctx"
	"github.com/pingcap/tidb/store/mockstore"
	"github.com/pingcap/tidb/table"
	"github.com/pingcap/tidb/testkit"
	"github.com/stretchr/testify/require"
)

func checkTableCacheStatus(t *testing.T, se session.Session, dbName, tableName string, status model.TableCacheStatusType) {
	tb := testGetTableByNameT(t, se, dbName, tableName)
	dom := domain.GetDomain(se)
	err := dom.Reload()
	require.NoError(t, err)
	require.Equal(t, status, tb.Meta().TableCacheStatusType)
}

func testGetTableByNameT(t *testing.T, ctx sessionctx.Context, db, table string) table.Table {
	dom := domain.GetDomain(ctx)
	// Make sure the table schema is the new schema.
	err := dom.Reload()
	require.NoError(t, err)
	tbl, err := dom.InfoSchema().TableByName(model.NewCIStr(db), model.NewCIStr(table))
	require.NoError(t, err)
	return tbl
}

func TestAlterPartitionCache(t *testing.T) {
	store, clean := testkit.CreateMockStore(t)
	defer clean()

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test;")
	tk.MustExec("drop table if exists cache_partition_table;")
	tk.MustExec("create table cache_partition_table (a int, b int) partition by hash(a) partitions 3;")
	tk.MustGetErrCode("alter table cache_partition_table cache", errno.ErrOptOnCacheTable)
	defer tk.MustExec("drop table if exists cache_partition_table;")
	tk.MustExec("drop table if exists cache_partition_range_table;")
	tk.MustExec(`create table cache_partition_range_table (c1 smallint(6) not null, c2 char(5) default null) partition by range ( c1 ) (
			partition p0 values less than (10),
			partition p1 values less than (20),
			partition p2 values less than (30),
			partition p3 values less than (MAXVALUE)
	);`)
	tk.MustGetErrCode("alter table cache_partition_range_table cache;", errno.ErrOptOnCacheTable)
	defer tk.MustExec("drop table if exists cache_partition_range_table;")
	tk.MustExec("drop table if exists partition_list_table;")
	tk.MustExec("set @@session.tidb_enable_list_partition = ON")
	tk.MustExec(`create table cache_partition_list_table (id int) partition by list  (id) (
	    partition p0 values in (1,2),
	    partition p1 values in (3,4),
	    partition p3 values in (5,null)
	);`)
	tk.MustGetErrCode("alter table cache_partition_list_table cache", errno.ErrOptOnCacheTable)
	tk.MustExec("drop table if exists cache_partition_list_table;")
}

func TestAlterViewTableCache(t *testing.T) {
	store, clean := testkit.CreateMockStore(t)
	defer clean()

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test;")
	tk.MustExec("drop table if exists cache_view_t")
	tk.MustExec("create table cache_view_t (id int);")
	tk.MustExec("create view v as select * from cache_view_t")
	tk.MustGetErrCode("alter table v cache", errno.ErrWrongObject)
}

func TestAlterTableNoCache(t *testing.T) {
	store, clean := testkit.CreateMockStore(t)
	defer clean()

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("drop table if exists nocache_t1")
	/* Test of cache table */
	tk.MustExec("create table nocache_t1 ( n int auto_increment primary key)")
	tk.MustExec("alter table nocache_t1 cache")
	checkTableCacheStatus(t, tk.Session(), "test", "nocache_t1", model.TableCacheStatusEnable)
	tk.MustExec("alter table nocache_t1 nocache")
	checkTableCacheStatus(t, tk.Session(), "test", "nocache_t1", model.TableCacheStatusDisable)
	tk.MustExec("drop table if exists t1")
	// Test if a table is not exists
	tk.MustExec("drop table if exists nocache_t")
	tk.MustGetErrCode("alter table nocache_t cache", errno.ErrNoSuchTable)
	tk.MustExec("create table nocache_t (a int)")
	tk.MustExec("alter table nocache_t nocache")
	// Multiple no alter cache is okay
	tk.MustExec("alter table nocache_t nocache")
	tk.MustExec("alter table nocache_t nocache")
}

func TestIndexOnCacheTable(t *testing.T) {
	store, clean := testkit.CreateMockStore(t)
	defer clean()

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test;")
	/*Test cache table can't add/drop/rename index */
	tk.MustExec("drop table if exists cache_index")
	tk.MustExec("create table cache_index (c1 int primary key, c2 int, c3 int, index ok2(c2))")
	defer tk.MustExec("drop table if exists cache_index")
	tk.MustExec("alter table cache_index cache")
	tk.MustGetErrCode("create index cache_c2 on cache_index(c2)", errno.ErrOptOnCacheTable)
	tk.MustGetErrCode("alter table cache_index add index k2(c2)", errno.ErrOptOnCacheTable)
	tk.MustGetErrCode("alter table cache_index drop index ok2", errno.ErrOptOnCacheTable)
	/*Test rename index*/
	tk.MustGetErrCode("alter table cache_index rename index ok2 to ok", errno.ErrOptOnCacheTable)
	/*Test drop different indexes*/
	tk.MustExec("drop table if exists cache_index_1")
	tk.MustExec("create table cache_index_1 (id int, c1 int, c2 int, primary key(id), key i1(c1), key i2(c2));")
	tk.MustExec("alter table cache_index_1 cache")
	tk.MustGetErrCode("alter table cache_index_1 drop index i1, drop index i2;", errno.ErrOptOnCacheTable)
}

func TestAlterTableCache(t *testing.T) {
	store, err := mockstore.NewMockStore()
	require.NoError(t, err)
	session.SetSchemaLease(600 * time.Millisecond)
	session.DisableStats4Test()
	dom, err := session.BootstrapSession(store)
	require.NoError(t, err)

	dom.SetStatsUpdating(true)

	clean := func() {
		dom.Close()
		err := store.Close()
		require.NoError(t, err)
	}
	defer clean()
	tk := testkit.NewTestKit(t, store)
	tk2 := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t1")
	tk2.MustExec("use test")
	/* Test of cache table */
	tk.MustExec("create table t1 ( n int auto_increment primary key)")
	tk.MustGetErrCode("alter table t1 ca", errno.ErrParse)
	tk.MustGetErrCode("alter table t2 cache", errno.ErrNoSuchTable)
	tk.MustExec("alter table t1 cache")
	checkTableCacheStatus(t, tk.Session(), "test", "t1", model.TableCacheStatusEnable)
	tk.MustExec("drop table if exists t1")
	/*Test can't skip schema checker*/
	tk.MustExec("drop table if exists t1,t2")
	tk.MustExec("CREATE TABLE t1 (a int)")
	tk.MustExec("CREATE TABLE t2 (a int)")
	tk.MustExec("begin")
	tk.MustExec("insert into t1 set a=1;")
	tk2.MustExec("alter table t1 cache;")
	_, err = tk.Exec("commit")
	require.True(t, terror.ErrorEqual(domain.ErrInfoSchemaChanged, err))
	/* Test can skip schema checker */
	tk.MustExec("begin")
	tk.MustExec("drop table if exists t1")
	tk.MustExec("CREATE TABLE t1 (a int)")
	tk.MustExec("insert into t1 set a=2;")
	tk2.MustExec("alter table t2 cache")
	tk.MustExec("commit")
	// Test if a table is not exists
	tk.MustExec("drop table if exists t")
	tk.MustGetErrCode("alter table t cache", errno.ErrNoSuchTable)
	tk.MustExec("create table t (a int)")
	tk.MustExec("alter table t cache")
	// Multiple alter cache is okay
	tk.MustExec("alter table t cache")
	tk.MustExec("alter table t cache")
	// Test a temporary table
	tk.MustExec("drop table if exists t")
	tk.MustExec("create temporary table t (id int primary key auto_increment, u int unique, v int)")
	tk.MustExec("drop table if exists tmp1")
	// local temporary table alter is not supported
	tk.MustGetErrCode("alter table t cache", errno.ErrUnsupportedDDLOperation)
	// test global temporary table
	tk.MustExec("create global temporary table tmp1 " +
		"(id int not null primary key, code int not null, value int default null, unique key code(code))" +
		"on commit delete rows")
	tk.MustGetErrMsg("alter table tmp1 cache", ddl.ErrOptOnTemporaryTable.GenWithStackByArgs("alter temporary table cache").Error())
}
