// Package data holds the authoritative Bangladesh administrative geography —
// 8 divisions and 64 districts — mirrored EXACTLY from the frontend's
// src/data/districts.ts so the server validates against the same source the
// cascading select is built from. The server is the source of truth: it never
// trusts a client-supplied division/district pair without checking it here.
//
// IsValidPair accepts either the Bengali or English label for each name,
// because the /apply form submits whichever language the applicant is using.
package data

import "strings"

type District struct {
	BN string `json:"bn"`
	EN string `json:"en"`
}

type Division struct {
	BN        string     `json:"bn"`
	EN        string     `json:"en"`
	Districts []District `json:"districts"`
}

// Divisions is the full 8-division / 64-district tree.
var Divisions = []Division{
	{BN: "ঢাকা", EN: "Dhaka", Districts: []District{
		{BN: "ঢাকা", EN: "Dhaka"}, {BN: "ফরিদপুর", EN: "Faridpur"}, {BN: "গাজীপুর", EN: "Gazipur"},
		{BN: "গোপালগঞ্জ", EN: "Gopalganj"}, {BN: "কিশোরগঞ্জ", EN: "Kishoreganj"}, {BN: "মাদারীপুর", EN: "Madaripur"},
		{BN: "মানিকগঞ্জ", EN: "Manikganj"}, {BN: "মুন্শিগঞ্জ", EN: "Munshiganj"}, {BN: "নারায়ণগঞ্জ", EN: "Narayanganj"},
		{BN: "নরসিংদী", EN: "Narsingdi"}, {BN: "রাজবাড়ী", EN: "Rajbari"}, {BN: "শরীয়তপুর", EN: "Shariatpur"},
		{BN: "টাঙ্গাইল", EN: "Tangail"},
	}},
	{BN: "চট্টগ্রাম", EN: "Chattogram", Districts: []District{
		{BN: "চট্টগ্রাম", EN: "Chattogram"}, {BN: "কক্সবাজার", EN: "Cox's Bazar"}, {BN: "কুমিল্লা", EN: "Cumilla"},
		{BN: "ব্রাহ্মণবাড়িয়া", EN: "Brahmanbaria"}, {BN: "চাঁদপুর", EN: "Chandpur"}, {BN: "ফেনী", EN: "Feni"},
		{BN: "খাগড়াছড়ি", EN: "Khagrachhari"}, {BN: "লক্ষ্মীপুর", EN: "Lakshmipur"}, {BN: "নোয়াখালী", EN: "Noakhali"},
		{BN: "রাঙ্গামাটি", EN: "Rangamati"}, {BN: "বান্দরবান", EN: "Bandarban"},
	}},
	{BN: "রাজশাহী", EN: "Rajshahi", Districts: []District{
		{BN: "রাজশাহী", EN: "Rajshahi"}, {BN: "বগুড়া", EN: "Bogura"}, {BN: "জয়পুরহাট", EN: "Joypurhat"},
		{BN: "নওগাঁ", EN: "Naogaon"}, {BN: "নাটোর", EN: "Natore"}, {BN: "চাঁপাইনবাবগঞ্জ", EN: "Chapainawabganj"},
		{BN: "পাবনা", EN: "Pabna"}, {BN: "সিরাজগঞ্জ", EN: "Sirajganj"},
	}},
	{BN: "খুলনা", EN: "Khulna", Districts: []District{
		{BN: "খুলনা", EN: "Khulna"}, {BN: "বাগেরহাট", EN: "Bagerhat"}, {BN: "চুয়াডাঙ্গা", EN: "Chuadanga"},
		{BN: "যশোর", EN: "Jashore"}, {BN: "ঝিনাইদহ", EN: "Jhenaidah"}, {BN: "কুষ্টিয়া", EN: "Kushtia"},
		{BN: "মাগুরা", EN: "Magura"}, {BN: "মেহেরপুর", EN: "Meherpur"}, {BN: "নড়াইল", EN: "Narail"},
		{BN: "সাতক্ষীরা", EN: "Satkhira"},
	}},
	{BN: "বরিশাল", EN: "Barishal", Districts: []District{
		{BN: "বরিশাল", EN: "Barishal"}, {BN: "বরগুনা", EN: "Barguna"}, {BN: "ভোলা", EN: "Bhola"},
		{BN: "ঝালকাঠি", EN: "Jhalokati"}, {BN: "পটুয়াখালী", EN: "Patuakhali"}, {BN: "পিরোজপুর", EN: "Pirojpur"},
	}},
	{BN: "সিলেট", EN: "Sylhet", Districts: []District{
		{BN: "সিলেট", EN: "Sylhet"}, {BN: "হবিগঞ্জ", EN: "Habiganj"}, {BN: "মৌলভীবাজার", EN: "Moulvibazar"},
		{BN: "সুনামগঞ্জ", EN: "Sunamganj"},
	}},
	{BN: "রংপুর", EN: "Rangpur", Districts: []District{
		{BN: "রংপুর", EN: "Rangpur"}, {BN: "দিনাজপুর", EN: "Dinajpur"}, {BN: "গাইবান্ধা", EN: "Gaibandha"},
		{BN: "কুড়িগ্রাম", EN: "Kurigram"}, {BN: "লালমনিরহাট", EN: "Lalmonirhat"}, {BN: "নীলফামারী", EN: "Nilphamari"},
		{BN: "পঞ্চগড়", EN: "Panchagarh"}, {BN: "ঠাকুরগাঁও", EN: "Thakurgaon"},
	}},
	{BN: "ময়মনসিংহ", EN: "Mymensingh", Districts: []District{
		{BN: "ময়মনসিংহ", EN: "Mymensingh"}, {BN: "জামালপুর", EN: "Jamalpur"}, {BN: "নেত্রকোণা", EN: "Netrokona"},
		{BN: "শেরপুর", EN: "Sherpur"},
	}},
}

// pairIndex maps a normalized (division, district) key to true. Built once at
// init from Divisions, keyed on both the BN and EN label of each name so a
// lookup succeeds regardless of the form's active language.
var pairIndex = func() map[string]bool {
	m := make(map[string]bool)
	for _, div := range Divisions {
		divKeys := []string{norm(div.BN), norm(div.EN)}
		for _, dist := range div.Districts {
			distKeys := []string{norm(dist.BN), norm(dist.EN)}
			for _, dk := range divKeys {
				for _, sk := range distKeys {
					m[dk+"|"+sk] = true
				}
			}
		}
	}
	return m
}()

// IsValidPair reports whether district belongs to division, matching on either
// the Bengali or English label of each.
func IsValidPair(division, district string) bool {
	return pairIndex[norm(division)+"|"+norm(district)]
}

// DistrictCount returns the total number of districts (should be 64).
func DistrictCount() int {
	n := 0
	for _, d := range Divisions {
		n += len(d.Districts)
	}
	return n
}

func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
