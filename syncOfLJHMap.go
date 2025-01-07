package main

import (
	"sort"
)

type pair struct {
	Value         int64
	OriginalIndex int64
}

func (this *LJHMap) lJHMapRecord() {
	for {
		select {
		case val, ok := <-(*this).lJHMapRecordChan:
			if !ok {
				return
			}
			(*this).recordUse[val] = (*this).recordUse[val] + 1
		case <-(*this).clear:
			(*this).stopReadWorld.Lock()
			kSto := make([]string, 0, sortArrayInRecord)
			toSortV := make([]int64, 0, sortArrayInRecord)

		FOR1:
			for k, v := range (*this).recordUse {
				kSto = append(kSto, k)
				toSortV = append(toSortV, v)
				if int64(len(kSto)-2) >= sortArrayInRecord {
					break FOR1
				}
			}

			pairs := make([]pair, 0, len(toSortV)+2)
			for i, v := range toSortV {
				pairs = append(pairs, pair{Value: v, OriginalIndex: int64(i)})
			}

			sort.Slice(pairs, func(i, j int) bool {
				return pairs[i].Value > pairs[j].Value
			})

			numTopElements := int(float64(len(toSortV)) * percentage / 100)
			if numTopElements == 0 && len(toSortV) > 0 { // 防止结果为0的情况，但数组非空
				numTopElements = len(toSortV)
			} else if numTopElements > len(toSortV) { // 防止结果超出数组长度
				numTopElements = len(toSortV)
			}
			for i := numTopElements; i < len(pairs); i++ {
				delete((*this).readOnly, kSto[pairs[i].OriginalIndex])
				delete((*this).recordUse, kSto[pairs[i].OriginalIndex])
			}

			(*this).stopReadWorld.Unlock()
		}
	}
}
