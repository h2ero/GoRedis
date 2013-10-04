package goredis_server

import (
	. "../goredis"
	"./monitor"
	. "./storage"
	"container/list"
	"errors"
	"strings"
	"sync"
)

var (
	WrongKindError = errors.New("Wrong kind opration")
	WrongKindReply = ErrorReply(WrongKindError)
)

// 数据类型描述
var entryTypeDesc = map[EntryType]string{
	EntryTypeUnknown:   "unknown",
	EntryTypeString:    "string",
	EntryTypeHash:      "hash",
	EntryTypeList:      "list",
	EntryTypeSet:       "set",
	EntryTypeSortedSet: "zset"}

// 命令类型集合
var cmdCategory = map[string][]string{
	"string": []string{"GET", "SET", "INCR", "DECR", "INCRBY", "DECRBY", "MSET", "MGET"},
	"hash":   []string{"HDEL", "HGET", "HSET", "HMGET", "HMSET", "HGETALL", "HINCRBY", "HKEYS", "HLEN"},
	"list":   []string{"LINDEX", "LLEN", "LPOP", "LPUSH", "LRANGE", "LREM", "RPOP", "RPUSH"},
	"set":    []string{"SADD", "SCARD", "SISMEMBER", "SMEMBERS", "SREM"},
	"zset":   []string{"ZADD", "ZCARD", "ZINCRBY", "ZRANGE", "ZRANGEBYSCORE", "ZREM", "ZREMRANGEBYRANK", "ZREMRANGEBYSCORE", "ZREVRANGE", "ZREVRANGEBYSCORE", "ZSCORE"},
}

// 需要同步到从库的命令
var needSyncCmds = []string{
	"SET", "INCR", "DECR", "INCRBY", "DECRBY", "MSET",
	"HDEL", "HSET", "HMSET", "HINCRBY",
	"LPOP", "LPUSH", "LREM", "RPOP", "RPUSH",
	"SADD", "SREM",
	"ZADD", "ZINCRBY", "ZREM",
	"DEL"}

// GoRedisServer
type GoRedisServer struct {
	CommandHandler
	RedisServer
	// 数据源
	directory  string
	datasource DataSource
	// counters
	cmdCounters  *monitor.Counters
	syncCounters *monitor.Counters
	// logger
	statusLogger *monitor.StatusLogger
	syncMonitor  *monitor.StatusLogger
	// 从库
	slavelist        *list.List
	needSyncCmdTable map[string]bool // 需要同步的指令
	// locks
	stringMutex sync.Mutex
}

func NewGoRedisServer(directory string) (server *GoRedisServer) {
	server = &GoRedisServer{}
	// set as itself
	server.SetHandler(server)
	// default datasource
	server.directory = directory
	var e1 error
	server.datasource, e1 = NewLevelDBDataSource(server.directory + "/db0")
	if e1 != nil {
		panic(e1)
	}
	// server.datasource = NewMemoryDataSource()
	// counter
	server.cmdCounters = monitor.NewCounters()
	server.syncCounters = monitor.NewCounters()
	// monitor
	server.initCommandMonitor(server.directory + "/cmd.log")
	server.initSyncMonitor(server.directory + "/sync.log")
	// slave
	server.slavelist = list.New()
	server.needSyncCmdTable = make(map[string]bool)
	for _, cmd := range needSyncCmds {
		server.needSyncCmdTable[strings.ToUpper(cmd)] = true
	}
	return
}

func (server *GoRedisServer) Listen(host string) {
	server.RedisServer.Listen(host)
}

// 命令执行监控
func (server *GoRedisServer) initCommandMonitor(path string) {
	// monitor
	server.statusLogger = monitor.NewStatusLogger(path)
	server.statusLogger.Add(monitor.NewTimeFormater("Time", 8))
	cmds := []string{"TOTAL", "GET", "SET", "HSET", "HGET", "HGETALL", "INCR", "DEL", "ZADD"}
	for _, cmd := range cmds {
		padding := len(cmd) + 1
		if padding < 7 {
			padding = 7
		}
		server.statusLogger.Add(monitor.NewCountFormater(server.cmdCounters.Get(cmd), cmd, padding))
	}
	server.statusLogger.Start()
}

// 从库同步监控
func (server *GoRedisServer) initSyncMonitor(path string) {
	server.syncMonitor = monitor.NewStatusLogger(path)
	server.syncMonitor.Add(monitor.NewTimeFormater("Time", 8))
	cmds := []string{"total", "string", "hash", "set", "list", "zset", "ping"}
	for _, cmd := range cmds {
		server.syncMonitor.Add(monitor.NewCountFormater(server.syncCounters.Get(cmd), cmd, 8))
	}
	server.syncMonitor.Start()
}

// for CommandHandler
func (server *GoRedisServer) On(cmd *Command, session *Session) {
	go func() {
		cmdName := strings.ToUpper(cmd.Name())
		server.cmdCounters.Get(cmdName).Incr(1)
		server.cmdCounters.Get("TOTAL").Incr(1)

		// 同步到从库
		if _, ok := server.needSyncCmdTable[cmdName]; ok {
			for e := server.slavelist.Front(); e != nil; e = e.Next() {
				e.Value.(*SlaveSession).SendCommand(cmd)
			}
		}
	}()
}

func (server *GoRedisServer) OnUndefined(cmd *Command, session *Session) (reply *Reply) {
	return ErrorReply("Not Supported: " + cmd.String())
}
