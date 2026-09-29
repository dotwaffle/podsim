#!/usr/bin/env python3
"""Convert pinned TfL files into LondonFull topology, demand, and provenance."""

import argparse
import collections
import csv
from decimal import Decimal
import hashlib
import json
import math
from pathlib import Path
import re
import xml.etree.ElementTree as ET
import zipfile

HERE = Path(__file__).resolve().parent
MERGES = {"940GZZLUMMT": "940GZZLUBNK", "940GZZLUPAH": "940GZZLUPAC", "940GZZLUHSC": "940GZZLUHSD"}
BANDS = ["early", "morning", "am_peak", "inter_peak", "pm_peak", "evening", "late", "night"]
NS = {"x": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")


def verify_sources(root, manifest):
    for entry in manifest:
        data = (root / entry["file"]).read_bytes()
        require(len(data) == entry["bytes"] and hashlib.sha256(data).hexdigest() == entry["sha256"],
                "source hash mismatch: " + entry["file"])


def sheets(path, selected):
    with zipfile.ZipFile(path) as archive:
        texts = ["".join(n.itertext()) for n in ET.fromstring(archive.read("xl/sharedStrings.xml")).findall("x:si", NS)]
        relations = {n.attrib["Id"]: n.attrib["Target"] for n in ET.fromstring(archive.read("xl/_rels/workbook.xml.rels"))}
        for sheet in ET.fromstring(archive.read("xl/workbook.xml")).findall("x:sheets/x:sheet", NS):
            name = sheet.attrib["name"]
            if name not in selected:
                continue
            link = sheet.attrib["{http://schemas.openxmlformats.org/officeDocument/2006/relationships}id"]
            rows = []
            for row in ET.fromstring(archive.read("xl/" + relations[link])).findall("x:sheetData/x:row", NS):
                cells = {}
                for cell in row.findall("x:c", NS):
                    key = re.sub(r"[0-9]", "", cell.attrib["r"])
                    value = cell.findtext("x:v", "", NS)
                    cells[key] = texts[int(value)] if cell.attrib.get("t") == "s" else value
                rows.append(cells)
            yield name, rows


def normal_name(name):
    return re.sub(r"[^a-z0-9]", "", name.lower().replace(" underground station", "").replace("&", "and"))


def topology(root, manifest):
    stops, sequences = {}, []
    edges = collections.defaultdict(set)
    for entry in sorted(manifest, key=lambda x: x["file"]):
        if not entry["file"].endswith("bound.json"):
            continue
        data = json.loads((root / entry["file"]).read_text())
        require(data["mode"] == "tube", "non-Tube route")
        for seq in data["stopPointSequences"]:
            ids = []
            for stop in seq["stopPoint"]:
                item = {k: stop[k] for k in ("id", "name", "lat", "lon", "zone")}
                require(all(math.isfinite(item[k]) for k in ("lat", "lon")), "nonfinite coordinate")
                require(50 < item["lat"] < 53 and -1 < item["lon"] < 1, "coordinate outside London")
                require(item["id"] not in stops or stops[item["id"]] == item, "inconsistent source station")
                stops[item["id"]] = item
                ids.append(item["id"])
            sequences.append({"file": entry["file"], "line": data["lineId"], "direction": seq["direction"],
                              "branch": seq["branchId"], "next": seq["nextBranchIds"], "previous": seq["prevBranchIds"], "stops": ids})
            for a, b in zip(ids, ids[1:]):
                a, b = MERGES.get(a, a), MERGES.get(b, b)
                if a != b:
                    edges[tuple(sorted((a, b)))].add(data["lineId"])
    require(len(stops) == 272, "Tube roster count changed")
    ids = {MERGES.get(k, k) for k in stops}
    require(len(ids) == 269 and len(edges) == 313, "normalized topology count changed")
    sites = [stops[k].copy() for k in sorted(ids)]
    for site in sites:
        site["name"] = site["name"].removesuffix(" Underground Station")
        if site["id"] == "940GZZLUBNK":
            site["name"] = "Bank and Monument"
    return stops, {"source": {"retrieved": "2026-09-28", "url": "https://api.tfl.gov.uk/Line/Mode/tube",
                               "attribution": "Powered by TfL Open Data"}, "stations": sites,
                   "links": [{"a": a, "b": b, "lines": sorted(lines)} for (a, b), lines in sorted(edges.items())]}, sequences


def demand_mapping(root, stops, aliases):
    tables = dict(sheets(root / "numbat-2024-definitions.xlsx", {"Stations", "Stn-Naptan", "Stn-Mode"}))
    names = {r["A"]: r["C"] for r in tables["Stations"][1:] if "A" in r}
    naptan = {r["A"]: r.get("D", "") for r in tables["Stn-Naptan"][1:] if "A" in r}
    lu = {r["A"] for r in tables["Stn-Mode"][1:] if r.get("E") == "1"}
    require(len(aliases) == 39 and len({a["code"] for a in aliases}) == 39, "mixed-mode alias count changed")
    explicit = {}
    for alias in aliases:
        code, target = alias["code"], alias["selected"]
        require(code in lu and names[code] == alias["name"] and naptan[code] == alias["naptan"], "alias definition mismatch: " + code)
        require(target in stops and normal_name(names[code]) == normal_name(stops[target]["name"]), "alias roster mismatch: " + code)
        explicit[code] = target
    mapping, records, excluded = {}, [], []
    for code in sorted(lu):
        target = explicit.get(code, naptan[code])
        if target not in stops:
            excluded.append({"code": code, "name": names[code], "naptan": naptan[code]})
            continue
        site = MERGES.get(target, target)
        mapping[code] = site
        records.append({"code": code, "name": names[code], "primary_naptan": naptan[code], "source_id": target,
                        "site_id": site, "mixed_mode_alias": code in explicit})
    require(excluded == [{"code": "866", "name": "West India Quay", "naptan": "940GZZDLWIQ"}], "unreviewed LU definition exclusion")
    require(len(mapping) == 270 and len(set(mapping.values())) == 269, "demand coverage changed")
    return mapping, records, excluded


def demand(rows, mapping):
    weights = collections.defaultdict(lambda: [Decimal(0) for _ in BANDS])
    seen = set()
    counts = collections.Counter()
    for row in rows:
        counts["source_rows"] += 1
        a, b, band = row["mnlc_o"], row["mnlc_d"], int(row["tb_o"])
        weight = Decimal(row["vol"])
        require(1 <= band <= 8 and weight.is_finite() and weight > 0, "invalid source band or weight")
        key = (a, b, band)
        require(key not in seen, "duplicate source OD band")
        seen.add(key)
        if a not in mapping or b not in mapping:
            counts["excluded_endpoint_rows"] += 1
            continue
        a, b = mapping[a], mapping[b]
        if a == b:
            counts["same_site_rows"] += 1
            continue
        weights[a, b][band - 1] += weight
        counts["retained_rows"] += 1
    return weights, dict(counts)


def convert(root, output):
    manifest = json.loads((HERE / "sources.json").read_text())
    verify_sources(root, manifest)
    stops, network, sequences = topology(root, manifest)
    mapping, records, excluded = demand_mapping(root, stops, json.loads((HERE / "aliases.json").read_text()))
    with (root / "numbat-2024-twt.csv").open(newline="") as source:
        reader = csv.DictReader(source)
        require(reader.fieldnames == ["mnlc_o", "mnlc_d", "tb_o", "vol"], "OD header changed")
        weights, counts = demand(reader, mapping)
    require(len(weights) == 60996, "normalized OD pair count changed")
    require({k for pair in weights for k in pair} == set(mapping.values()), "station without positive demand")
    output.mkdir(parents=True, exist_ok=True)
    write_json(output / "london-full-tube.json", network)
    with (output / "london-full-od-2024.csv").open("w", newline="") as target:
        writer = csv.writer(target, lineterminator="\n")
        writer.writerow(["from", "to"] + BANDS)
        for (a, b), values in sorted(weights.items()):
            writer.writerow([a, b] + [format(w, "f") for w in values])
    write_json(output / "london-full-provenance.json", {
        "sources": manifest, "semantics": "2024 Tuesday-to-Thursday network journeys filtered to Tube endpoints, including journeys on other modes",
        "empty_source_bands": [1, 8], "available_source_bands": [2, 3, 4, 5, 6, 7],
        "band_note": "Source contains no Early or Night rows. Preserve their zero columns; do not offer them as positive-demand selections.",
        "merges": MERGES, "source_stations": [stops[k] | {"site_id": MERGES.get(k, k)} for k in sorted(stops)],
        "sequences": sequences, "demand_mapping": records, "excluded_definitions": excluded,
        "counts": counts | {"source_stations": len(stops), "sites": len(network["stations"]), "links": len(network["links"]), "od_pairs": len(weights)},
        "band_totals": [format(sum((w[i] for w in weights.values()), Decimal(0)), "f") for i in range(8)]})


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    convert(args.source, args.output)
