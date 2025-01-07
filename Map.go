package main

import (
	"errors"
	"sync"
	"sync/atomic"
)

const (
	thresholdOfUpdate   int64   = 20   // 多少次缓冲未命中 而清空newUpdateList去刷新缓冲区 的阈值
	updateReadCountMax  int64   = 10   // 多少次重写已在缓冲区的key的value 而清空overWriteUpdateList去刷新缓冲区 的阈值
	lJHMapRecordChanLen int64   = 1000 // 这是 读协程 向 清理协程 发送key的值 的通道 的容量, 如果读协程较多, 这个就多一点
	sortArrayInRecord   int64   = 1000 // 每次排序使用量的数组大小, 每次对缓存字典中任意多少个元素排序, 这个任意就是这个值
	percentage          float64 = 30.0 // 保留多少使用量较高的数据的比例 此处的float64不要动
)

type AtomicInt64 struct {
	value int64
}

func (a *AtomicInt64) Add(delta int64) int64 {
	return atomic.AddInt64(&((*a).value), delta)
}
func (a *AtomicInt64) Value() int64 {
	return atomic.LoadInt64(&((*a).value))
}
func (a *AtomicInt64) Set(newValue int64) {
	atomic.StoreInt64(&((*a).value), newValue)
}

type LJHMap struct {
	clear            chan struct{}    // 回收缓存空间信号
	recordUse        map[string]int64 // 只有专门处理record的协程才会用到
	lJHMapRecordChan chan string      // 命中一次缓存或更新一条已存在数据就向此通道发一次Key, 去记录哪些key用的多

	readOnly     map[string]interface{}
	readAndWrite map[string]interface{}

	// 以下四个字段配合开头的常量就好理解了
	newUpdateCount      *AtomicInt64 // 这里指的是未命中缓冲区而从readAndWrite读取的次数
	overWriteCount      *AtomicInt64 // 这里指的是一次写操作要重复多少次才会被写入缓冲区
	newUpdateList       UpdateList   // 因未命中缓冲区而进入此列表而待更新的条目的缓存列表
	overWriteUpdateList UpdateList   // 待更新的条目的缓存列表

	deleteCount *AtomicInt64
	deleteList  DeleteList

	mx                    *sync.RWMutex // 混合读写的字典用
	stopReadWorld         *sync.RWMutex // 只有只读字典自己用
	allUpdateFuncUseCount *AtomicInt64  // 将newUpdateCount、updateReadCount清空并写入缓冲区的次数
	allUpdate             *AtomicInt64  // 更新缓冲区的次数记录 包括在缓冲区新建条目 更新条目的次数 都是每次newUpdateCount、updateReadCount清空前将它们的len加过来
	overWriteFuncUseCount *AtomicInt64  // 只包括updateReadCount清空并写入缓冲区的次数
}
type UpdateList struct {
	uListKey []string
	uListVal []interface{}
	mx       *sync.Mutex
}
type DeleteList struct {
	dListKey []string
	mx       *sync.Mutex
}

func NewLJHMap(num int) (re *LJHMap) {
	re = &LJHMap{
		clear:                 make(chan struct{}),
		lJHMapRecordChan:      make(chan string, lJHMapRecordChanLen), // 命中一次缓存或更新一条已存在数据就向此通道发一次Key, 去记录哪些key用的多
		recordUse:             make(map[string]int64, sortArrayInRecord),
		readOnly:              make(map[string]interface{}, num),
		readAndWrite:          make(map[string]interface{}, num),
		newUpdateCount:        &AtomicInt64{value: 0},
		deleteCount:           &AtomicInt64{value: 0},
		allUpdateFuncUseCount: &AtomicInt64{value: 0},
		allUpdate:             &AtomicInt64{value: 0},
		mx:                    &sync.RWMutex{},
		stopReadWorld:         &sync.RWMutex{},
		newUpdateList:         UpdateList{mx: &sync.Mutex{}},
		overWriteUpdateList:   UpdateList{mx: &sync.Mutex{}},
		deleteList:            DeleteList{mx: &sync.Mutex{}},
		overWriteCount:        &AtomicInt64{value: 0},
		overWriteFuncUseCount: &AtomicInt64{value: 0},
	}

	(*re).newUpdateCount.Set(0)
	(*re).deleteCount.Set(0)
	(*re).allUpdateFuncUseCount.Set(0)
	(*re).allUpdate.Set(0)
	(*re).overWriteCount.Set(0)
	(*re).overWriteFuncUseCount.Set(0)

	go (*re).lJHMapRecord() // 启动 用于记录和清洗缓冲 协程
	return re
}

func (this *LJHMap) UpdateToBrfNow() {
	if (*this).newUpdateCount.Value() >= thresholdOfUpdate {
		(*this).stopReadWorld.Lock()
		if (*this).newUpdateCount.Value() >= thresholdOfUpdate {
			(*this).newUpdateList.mx.Lock()
			lockBackup := (*this).newUpdateList.mx
			if len((*this).newUpdateList.uListKey) != len((*this).newUpdateList.uListVal) {
				panic("重写read缓存失败,1")
			}

			(*this).allUpdate.Add(int64(len((*this).newUpdateList.uListKey)))
			for i := 0; i < len((*this).newUpdateList.uListKey); i++ {
				(*this).readOnly[(*this).newUpdateList.uListKey[i]] = (*this).newUpdateList.uListVal[i]
			}

			(*this).newUpdateList = UpdateList{mx: lockBackup}
			(*this).newUpdateCount.Set(0)
			(*this).allUpdateFuncUseCount.Add(1)
			(*this).newUpdateList.mx.Unlock()
		}
		(*this).stopReadWorld.Unlock()
	}

	if (*this).overWriteCount.Value() >= updateReadCountMax {
		(*this).stopReadWorld.Lock()
		if (*this).overWriteCount.Value() >= updateReadCountMax {
			(*this).overWriteUpdateList.mx.Lock()
			lockBackup := (*this).overWriteUpdateList.mx
			if len((*this).overWriteUpdateList.uListKey) != len((*this).overWriteUpdateList.uListVal) {
				panic("重写read缓存失败,2")
			}

			(*this).allUpdate.Add(int64(len((*this).overWriteUpdateList.uListKey)))
			for i := 0; i < len((*this).overWriteUpdateList.uListKey); i++ {
				(*this).lJHMapRecordChan <- (*this).overWriteUpdateList.uListKey[i]
				(*this).readOnly[(*this).overWriteUpdateList.uListKey[i]] = (*this).overWriteUpdateList.uListVal[i]
			}

			(*this).overWriteUpdateList = UpdateList{mx: lockBackup}
			(*this).overWriteCount.Set(0)
			(*this).allUpdateFuncUseCount.Add(1)
			(*this).overWriteFuncUseCount.Add(1)
			(*this).overWriteUpdateList.mx.Unlock()
		}
		(*this).stopReadWorld.Unlock()
	}
}

func (this *LJHMap) ReadMap(key string, updateNow bool) (interface{}, error) {
	if updateNow {
		this.UpdateToBrfNow()
	}

	(*this).stopReadWorld.RLock()
	value, exist := (*this).readOnly[key]
	(*this).stopReadWorld.RUnlock()

	if !exist { // 如果缓存中没有
		(*this).mx.RLock()
		value1, exist1 := (*this).readAndWrite[key]
		(*this).mx.RUnlock()

		if !exist1 {
			return nil, errors.New("不存在")
		} else {
			(*this).newUpdateList.mx.Lock()
			(*this).newUpdateCount.Add(1)
			(*this).newUpdateList.uListKey = append((*this).newUpdateList.uListKey, key)
			(*this).newUpdateList.uListVal = append((*this).newUpdateList.uListVal, value1)
			(*this).newUpdateList.mx.Unlock()
			return value1, nil
		}
	} else {
		(*this).lJHMapRecordChan <- key
		return value, nil
	}
}

func (this *LJHMap) WriteMap(key string, val interface{}) {
	(*this).mx.Lock()
	_, exist := (*this).readAndWrite[key]
	(*this).readAndWrite[key] = val
	(*this).mx.Unlock()

	if exist {
		(*this).overWriteUpdateList.mx.Lock()
		(*this).overWriteCount.Add(1)
		(*this).overWriteUpdateList.uListKey = append((*this).overWriteUpdateList.uListKey, key)
		(*this).overWriteUpdateList.uListVal = append((*this).overWriteUpdateList.uListVal, val)
		(*this).overWriteUpdateList.mx.Unlock()
	}
}

func (this *LJHMap) DeleteMap(key string) {
	(*this).mx.Lock()
	delete((*this).readAndWrite, key)
	(*this).mx.Unlock()

	(*this).stopReadWorld.Lock()
	delete((*this).readOnly, key)
	(*this).stopReadWorld.Unlock()
}
func (this *LJHMap) StartClear() {
	(*this).clear <- struct{}{}
}

func (this *LJHMap) UpdateToBrfNowByParams(tthresholdOfUpdate int64, uupdateReadCountMax int64) {
	if (*this).newUpdateCount.Value() >= tthresholdOfUpdate {
		(*this).stopReadWorld.Lock()
		if (*this).newUpdateCount.Value() >= tthresholdOfUpdate {
			(*this).newUpdateList.mx.Lock()
			lockBackup := (*this).newUpdateList.mx
			if len((*this).newUpdateList.uListKey) != len((*this).newUpdateList.uListVal) {
				panic("重写read缓存失败,1")
			}

			(*this).allUpdate.Add(int64(len((*this).newUpdateList.uListKey)))
			for i := 0; i < len((*this).newUpdateList.uListKey); i++ {
				(*this).readOnly[(*this).newUpdateList.uListKey[i]] = (*this).newUpdateList.uListVal[i]
			}

			(*this).newUpdateList = UpdateList{mx: lockBackup}
			(*this).newUpdateCount.Set(0)
			(*this).allUpdateFuncUseCount.Add(1)
			(*this).newUpdateList.mx.Unlock()
		}
		(*this).stopReadWorld.Unlock()
	}

	if (*this).overWriteCount.Value() >= uupdateReadCountMax {
		(*this).stopReadWorld.Lock()
		if (*this).overWriteCount.Value() >= uupdateReadCountMax {
			(*this).overWriteUpdateList.mx.Lock()
			lockBackup := (*this).overWriteUpdateList.mx
			if len((*this).overWriteUpdateList.uListKey) != len((*this).overWriteUpdateList.uListVal) {
				panic("重写read缓存失败,2")
			}

			(*this).allUpdate.Add(int64(len((*this).overWriteUpdateList.uListKey)))
			for i := 0; i < len((*this).overWriteUpdateList.uListKey); i++ {
				(*this).lJHMapRecordChan <- (*this).overWriteUpdateList.uListKey[i]
				(*this).readOnly[(*this).overWriteUpdateList.uListKey[i]] = (*this).overWriteUpdateList.uListVal[i]
			}

			(*this).overWriteUpdateList = UpdateList{mx: lockBackup}
			(*this).overWriteCount.Set(0)
			(*this).allUpdateFuncUseCount.Add(1)
			(*this).overWriteFuncUseCount.Add(1)
			(*this).overWriteUpdateList.mx.Unlock()
		}
		(*this).stopReadWorld.Unlock()
	}
}
