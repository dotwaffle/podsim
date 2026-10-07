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
DATA = HERE.parents[1] / "internal/scenarios/data"
MERGES = {"940GZZLUMMT": "940GZZLUBNK", "940GZZLUPAH": "940GZZLUPAC", "940GZZLUHSC": "940GZZLUHSD"}
DLR_FILES = {"dlr-inbound.json", "dlr-outbound.json"}
BANDS = ["early", "morning", "am_peak", "inter_peak", "pm_peak", "evening", "late", "night"]
NS = {"x": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
SUFFIXES = (" Underground Station", " DLR Station")


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
    name = name.lower().replace(" underground station", "").replace(" dlr station", "")
    return re.sub(r"[^a-z0-9]", "", name.replace("&", "and"))


def distance(a, b):
    """Return the approximate distance between two stops in meters."""
    y = math.radians(b["lat"] - a["lat"])
    x = math.radians(b["lon"] - a["lon"]) * math.cos(math.radians((a["lat"] + b["lat"]) / 2))
    return round(6371000 * math.hypot(x, y))


def read_routes(root, manifest, mode, label):
    """Read route files of one mode. Return stops, hubs, and ordered sequences."""
    stops, hubs, sequences = {}, {}, []
    for entry in sorted(manifest, key=lambda x: x["file"]):
        data = json.loads((root / entry["file"]).read_text())
        require(data["mode"] == mode, "non-" + label + " route")
        for seq in data["stopPointSequences"]:
            ids = []
            for stop in seq["stopPoint"]:
                item = {k: stop[k] for k in ("id", "name", "lat", "lon", "zone")}
                require(all(math.isfinite(item[k]) for k in ("lat", "lon")), "nonfinite coordinate")
                require(50 < item["lat"] < 53 and -1 < item["lon"] < 1, "coordinate outside London")
                require(item["id"] not in stops or stops[item["id"]] == item, "inconsistent source station")
                stops[item["id"]] = item
                hub = stop.get("topMostParentId", item["id"])
                require(item["id"] not in hubs or hubs[item["id"]] == hub, "inconsistent source hub")
                hubs[item["id"]] = hub
                ids.append(item["id"])
            sequences.append({"file": entry["file"], "line": data["lineId"], "direction": seq["direction"],
                              "branch": seq["branchId"], "next": seq["nextBranchIds"], "previous": seq["prevBranchIds"], "stops": ids})
    return stops, hubs, sequences


def links(sequences, site):
    edges = collections.defaultdict(set)
    for seq in sequences:
        ids = seq["stops"]
        for a, b in zip(ids, ids[1:]):
            a, b = site[a], site[b]
            if a != b:
                edges[tuple(sorted((a, b)))].add(seq["line"])
    return edges


def site_record(stop):
    site = stop.copy()
    for suffix in SUFFIXES:
        site["name"] = site["name"].removesuffix(suffix)
    if site["id"] == "940GZZLUBNK":
        site["name"] = "Bank and Monument"
    return site


def network(source, stops, site, edges):
    return {"source": source, "stations": [site_record(stops[k]) for k in sorted(set(site.values()))],
            "links": [{"a": a, "b": b, "lines": sorted(lines)} for (a, b), lines in sorted(edges.items())]}


def route_entries(manifest, dlr):
    return [e for e in manifest if e["file"].endswith("bound.json") and (e["file"] in DLR_FILES) == dlr]


def tube_manifest(manifest):
    return [e for e in manifest if e["file"] not in DLR_FILES]


def topology(root, manifest):
    provenance = json.loads((DATA / "london-full-provenance.json").read_text())
    stops, hubs, sequences = pinned_tube(root, manifest, provenance)
    require(len(stops) == 272, "Tube roster count changed")
    site = {k: MERGES.get(k, k) for k in stops}
    edges = links(sequences, site)
    require(len(set(site.values())) == 269 and len(edges) == 313, "normalized topology count changed")
    return stops, network({"retrieved": "2026-09-28", "url": "https://api.tfl.gov.uk/Line/Mode/tube",
                           "attribution": "Powered by TfL Open Data"}, stops, site, edges), sequences, hubs


def definitions(root):
    tables = dict(sheets(root / "numbat-2024-definitions.xlsx", {"Stations", "Stn-Naptan", "Stn-Mode"}))
    names = {r["A"]: r["C"] for r in tables["Stations"][1:] if "A" in r}
    naptan = {r["A"]: r.get("D", "") for r in tables["Stn-Naptan"][1:] if "A" in r}
    lu = {r["A"] for r in tables["Stn-Mode"][1:] if r.get("E") == "1"}
    dlr = {r["A"] for r in tables["Stn-Mode"][1:] if r.get("F") == "1"}
    return names, naptan, lu, dlr


def check_aliases(aliases, modes, names, naptan, stops):
    explicit = {}
    for alias in aliases:
        code, target = alias["code"], alias["selected"]
        require(code in modes and names[code] == alias["name"] and naptan[code] == alias["naptan"], "alias definition mismatch: " + code)
        require(target in stops and normal_name(names[code]) == normal_name(stops[target]["name"]), "alias roster mismatch: " + code)
        explicit[code] = target
    return explicit


def map_codes(codes, explicit, names, naptan, site):
    mapping, records, excluded = {}, [], []
    for code in sorted(codes):
        target = explicit.get(code, naptan[code])
        if target not in site:
            excluded.append({"code": code, "name": names[code], "naptan": naptan[code]})
            continue
        mapping[code] = site[target]
        records.append({"code": code, "name": names[code], "primary_naptan": naptan[code], "source_id": target,
                        "site_id": site[target], "mixed_mode_alias": code in explicit})
    return mapping, records, excluded


def demand_mapping(root, stops, aliases):
    names, naptan, lu, _ = definitions(root)
    require(len(aliases) == 39 and len({a["code"] for a in aliases}) == 39, "mixed-mode alias count changed")
    explicit = check_aliases(aliases, lu, names, naptan, stops)
    mapping, records, excluded = map_codes(lu, explicit, names, naptan, {k: MERGES.get(k, k) for k in stops})
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


def read_demand(root, mapping):
    with (root / "numbat-2024-twt.csv").open(newline="") as source:
        reader = csv.DictReader(source)
        require(reader.fieldnames == ["mnlc_o", "mnlc_d", "tb_o", "vol"], "OD header changed")
        return demand(reader, mapping)


def write_demand(path, weights):
    with path.open("w", newline="") as target:
        writer = csv.writer(target, lineterminator="\n")
        writer.writerow(["from", "to"] + BANDS)
        for (a, b), values in sorted(weights.items()):
            writer.writerow([a, b] + [format(w, "f") for w in values])


def band_totals(weights):
    return [format(sum((w[i] for w in weights.values()), Decimal(0)), "f") for i in range(8)]


def band_fields():
    return {"empty_source_bands": [1, 8], "available_source_bands": [2, 3, 4, 5, 6, 7],
            "band_note": "Source contains no Early or Night rows. Preserve their zero columns; do not offer them as positive-demand selections."}


def convert(root, output):
    manifest = tube_manifest(json.loads((HERE / "sources.json").read_text()))
    verify_sources(root, [e for e in manifest if not e["file"].endswith("bound.json")])
    stops, tube, sequences, hubs = topology(root, manifest)
    mapping, records, excluded = demand_mapping(root, stops, json.loads((HERE / "aliases.json").read_text()))
    weights, counts = read_demand(root, mapping)
    require(len(weights) == 60996, "normalized OD pair count changed")
    require({k for pair in weights for k in pair} == set(mapping.values()), "station without positive demand")
    output.mkdir(parents=True, exist_ok=True)
    write_json(output / "london-full-tube.json", tube)
    write_demand(output / "london-full-od-2024.csv", weights)
    write_json(output / "london-full-provenance.json", {
        "sources": manifest, "semantics": "2024 Tuesday-to-Thursday network journeys filtered to Tube endpoints, including journeys on other modes"}
        | band_fields() | {
        "merges": MERGES, "source_stations": [stops[k] | {"site_id": MERGES.get(k, k), "hub": hubs[k]} for k in sorted(stops)],
        "sequences": sequences, "demand_mapping": records, "excluded_definitions": excluded,
        "counts": counts | {"source_stations": len(stops), "sites": len(tube["stations"]), "links": len(tube["links"]), "od_pairs": len(weights)},
        "band_totals": band_totals(weights)})


def pinned_tube(root, manifest, provenance):
    """Read the Tube routes and require the semantics pinned by the Tube provenance.

    TfL responses change in fields that conversion does not read. Both modes
    accept such files when stops, hub IDs, and sequences are unchanged.
    """
    require(provenance["sources"] == tube_manifest(manifest), "Tube provenance does not match the manifest")
    stops, hubs, sequences = read_routes(root, route_entries(manifest, False), "tube", "Tube")
    pinned = {s["id"]: {k: s[k] for k in ("id", "name", "lat", "lon", "zone")} for s in provenance["source_stations"]}
    pinned_hubs = {s["id"]: s["hub"] for s in provenance["source_stations"]}
    require(stops == pinned and hubs == pinned_hubs and sequences == provenance["sequences"], "Tube route semantics changed")
    return stops, hubs, sequences


def dlr_sites(tube_site, tube_stops, tube_hubs, dlr_stops, dlr_hubs, review, definition):
    """Map each DLR stop to a site. Return the site map and the merge records.

    A merge needs one NUMBAT code with LU and DLR modes and a NaPTAN hub
    shared with the Tube site. A DLR stop that shares a hub with a Tube
    stop must have a reviewed merge or separate entry.
    """
    names, naptan, lu, dlr, explicit = definition
    merged = {m["stop"]: m for m in review["merges"]}
    separate = {s["stop"]: s for s in review["separate"]}
    require(len(merged) == len(review["merges"]) and len(separate) == len(review["separate"]) and not merged.keys() & separate.keys(),
            "duplicate DLR review entry")
    site, records = {}, []
    for stop in sorted(dlr_stops):
        shared = sorted({tube_site[k] for k, hub in tube_hubs.items() if hub == dlr_hubs[stop] != stop})
        entry = merged.get(stop) or separate.get(stop)
        require(not shared or entry, "unreviewed DLR hub: " + stop)
        require(not entry or entry["name"] == dlr_stops[stop]["name"], "DLR review name mismatch: " + stop)
        if stop not in merged:
            site[stop] = stop
            continue
        merge = merged[stop]
        code, target = merge["code"], merge["site"]
        require(code in lu and code in dlr and tube_site.get(explicit.get(code, naptan[code])) == target, "DLR merge code mismatch: " + stop)
        require(merge["hub"] == dlr_hubs[stop] and shared == [target], "DLR merge hub mismatch: " + stop)
        site[stop] = target
        records.append(merge | {"code_name": names[code], "distance_m": distance(dlr_stops[stop], tube_stops[target])})
    require(set(merged) <= set(dlr_stops) and set(separate) <= set(dlr_stops), "DLR review stop outside the roster")
    return site, records


def omitted_dlr_sites(review, stops, site):
    """Return reviewed guideway bypasses for stops excluded from demand."""
    bypass = {}
    for entry in review["omitted"]:
        stop, target = entry["stop"], entry["connect_to"]
        require(stop not in bypass and stop in stops and site.get(stop) == stop, "invalid DLR omission: " + stop)
        require(entry["name"] == stops[stop]["name"] and entry["reason"], "DLR omission review mismatch: " + stop)
        require(target != stop and target in site and site[target] == target, "invalid DLR bypass: " + stop)
        bypass[stop] = target
    require(not bypass.keys() & set(bypass.values()), "DLR bypass targets an omitted stop")
    return bypass


def nearest(stop, sites):
    best = min(sites, key=lambda s: (distance(stop, s), s["id"]))
    return {"nearest_tube_site": best["id"], "nearest_tube_distance_m": distance(stop, best)}


def convert_dlr(root, output):
    manifest = json.loads((HERE / "sources.json").read_text())
    verify_sources(root, [e for e in manifest if not e["file"].endswith("bound.json") or e["file"] in DLR_FILES])
    tube_provenance = json.loads((DATA / "london-full-provenance.json").read_text())
    tube_stops, tube_hubs, tube_sequences = pinned_tube(root, manifest, tube_provenance)
    dlr_stops, dlr_hubs, dlr_sequences = read_routes(root, route_entries(manifest, True), "dlr", "DLR")
    require(len(dlr_stops) == 45 and not dlr_stops.keys() & tube_stops.keys(), "DLR roster count changed")
    review = json.loads((HERE / "dlr.json").read_text())
    names, naptan, lu, dlr = definitions(root)
    tube_aliases = json.loads((HERE / "aliases.json").read_text())
    require(len(tube_aliases) == 39 and len(review["aliases"]) == 2, "mixed-mode alias count changed")
    explicit = check_aliases(tube_aliases, lu, names, naptan, tube_stops)
    tube_site = {k: MERGES.get(k, k) for k in tube_stops}
    dlr_site, merges = dlr_sites(tube_site, tube_stops, tube_hubs, dlr_stops, dlr_hubs, review, (names, naptan, lu, dlr, explicit))
    site = tube_site | dlr_site
    stops = tube_stops | dlr_stops
    bypass = omitted_dlr_sites(review, dlr_stops, dlr_site)
    edges = links(tube_sequences + dlr_sequences, site | bypass)
    site = {k: v for k, v in site.items() if k not in bypass}
    full = network({"retrieved": "2026-09-28 (Tube), 2026-10-07 (DLR)", "url": "https://api.tfl.gov.uk/Line/Mode/tube,dlr",
                    "attribution": "Powered by TfL Open Data"}, stops, site, edges)
    require(len(merges) == 4 and len(full["stations"]) == 309 and len(full["links"]) == 358, "normalized DLR topology count changed")

    explicit |= check_aliases(review["aliases"], dlr, names, naptan, dlr_stops)
    mapping, records, excluded = map_codes(lu | dlr, explicit, names, naptan, site)
    require(excluded == [{"code": "866", "name": "West India Quay", "naptan": "940GZZDLWIQ"}], "unreviewed LU or DLR definition exclusion")
    require(len(mapping) == 310 and set(mapping.values()) == set(site.values()), "demand coverage changed")
    weights, counts = read_demand(root, mapping)
    require(len(weights) == 76567, "normalized OD pair count changed")
    require({k for pair in weights for k in pair} == set(mapping.values()), "station without positive demand")

    tube_sites = [s for s in full["stations"] if s["id"] in set(tube_site.values())]
    output.mkdir(parents=True, exist_ok=True)
    write_json(output / "london-full-dlr.json", full)
    write_demand(output / "london-full-dlr-od-2024.csv", weights)
    write_json(output / "london-full-dlr-provenance.json", {
        "sources": manifest,
        "tube_route_semantics": "Tube stops, hub IDs, and sequences must equal london-full-provenance.json. Later TfL responses can differ in fields that conversion does not read.",
        "semantics": "2024 Tuesday-to-Thursday network journeys filtered to Tube and DLR endpoints, including journeys on other modes"}
        | band_fields() | {
        "tube_merges": MERGES, "dlr_merges": merges, "dlr_separate": review["separate"], "dlr_omitted": review["omitted"],
        "definition_note": "Stn-Mode lists code 866, West India Quay, twice: one row has the LU flag and one has the DLR flag. The DLR conversion excludes both rows under the reviewed layout omission.",
        "dlr_source_stations": [dlr_stops[k] | {"hub": dlr_hubs[k], "site_id": site.get(k)} | nearest(dlr_stops[k], tube_sites) for k in sorted(dlr_stops)],
        "dlr_sequences": dlr_sequences, "demand_mapping": records, "excluded_definitions": excluded,
        "counts": counts | {"tube_source_stations": len(tube_stops), "dlr_source_stations": len(dlr_stops), "dlr_merges": len(merges),
                            "sites": len(full["stations"]), "dlr_sites": len(full["stations"]) - len(tube_sites), "links": len(full["links"]),
                            "dlr_links": sum("dlr" in link["lines"] for link in full["links"]), "od_pairs": len(weights)},
        "band_totals": band_totals(weights)})


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dlr", action="store_true", help="write the Tube and DLR files instead of the Tube files")
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    (convert_dlr if args.dlr else convert)(args.source, args.output)
