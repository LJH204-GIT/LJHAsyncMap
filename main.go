package main

import (
	"fmt"
	"strconv"
	"time"
)

func main() {
	stime := time.Now()
	a := NewLJHMap(9999999)

	start := 0
	init := true
	for i := 0; i < 1000; i++ {
		end := start + 9999
		start = end

		if init {
			for i111 := start - 9999; i111 < end; i111++ {
				a.WriteMap(strconv.Itoa(i111), i111)
			}
			init = false
			continue
		}
		go func(a1 int, b int) {

			for i222 := a1 - 9999; i222 < b; i222++ {
				a.WriteMap(strconv.Itoa(i222), i222)
			}
		}(start, end)

	}

	for i := 0; i < 1000; i++ {
		go func() {

			_, e := a.ReadMap(strconv.Itoa(i), true)
			if e == nil {
			}

		}()
	}
	for i := 0; i < 30; i++ {
		go func() {

			_, e := a.ReadMap(strconv.Itoa(i), true)
			if e == nil {
			}

		}()
	}
	for i := 0; i < 30; i++ {
		go func() {

			_, e := a.ReadMap(strconv.Itoa(i), true)
			if e == nil {
			}

		}()
	}

	for i := 0; i < 3000; i++ {
		go func() {
			a.WriteMap(strconv.Itoa(i), "{}")
		}()
	}

	for i := 0; i < 1000; i++ {
		go func() {

			_, e := a.ReadMap(strconv.Itoa(i), true)
			if e == nil {
			}

		}()
	}
	for i := 0; i < 1000; i++ {
		go func() {

			v, e := a.ReadMap(strconv.Itoa(i), true)
			if e == nil {
				fmt.Println(v)
			}

		}()
	}

	v, e := a.ReadMap(strconv.Itoa(90000), true)
	if e == nil {
		fmt.Println("   ", v)
	}
	a.StartClear()
	a.StartClear()
	a.StartClear()

	allTime := time.Since(stime)
	fmt.Println("====================", allTime.String())
	time.Sleep(1000 * time.Second)

}
