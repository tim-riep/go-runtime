// Copyright 2026 The go-runtime Authors
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


import { ContentTile, MetricTile, Tabs, Tile, TileGroups, type TileGroupData } from '@tim-riep/trui';
import { useEffect, useState } from 'react';
import { type Home as HomeData, type HomeSpace, type HomeTile, readTileValue } from './api.ts';
import { defined } from './defined.ts';
import { iconOf } from './icons.ts';
import { routeHash } from './route.ts';
import type { TextKey } from './texts.ts';

export interface HomeProps {
  home: HomeData;
  texts: Record<TextKey, string>;
  space: string | null;
  onSpaceChange: (space: string) => void;
  onOpen: (app: string) => void;
}

const tones = { NEUTRAL: 'neutral', GOOD: 'good', CRITICAL: 'critical', BAD: 'bad' } as const;

/** A metric tile, whose value is loaded when it is shown. */
function Metric({ tile, onOpen }: { tile: HomeTile; onOpen: (app: string) => void }) {
  const [value, setValue] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let current = true;
    readTileValue(tile.NAME).then(
      (v) => current && setValue(v),
      () => current && setFailed(true),
    );
    return () => {
      current = false;
    };
  }, [tile.NAME]);
  return (
    <MetricTile
      {...defined({
        subtitle: tile.SUBTITLE ?? undefined,
        footer: tile.FOOTER ?? undefined,
        icon: iconOf(tile.ICON),
        unit: tile.UNIT ?? undefined,
        tone: tile.TONE ? tones[tile.TONE] : undefined,
      })}
      tileId={tile.NAME}
      title={tile.TITLE}
      size={tile.SIZE === 'WIDE' ? '2x1' : '1x1'}
      value={value ?? '0'}
      state={failed ? 'failed' : value === null ? 'loading' : 'normal'}
      href={routeHash({ kind: 'app', app: tile.APP, params: {}, entity: null })}
      onPress={(e) => {
        e.preventDefault();
        onOpen(tile.APP);
      }}
    />
  );
}

export function TileOf({ tile, onOpen }: { tile: HomeTile; onOpen: (app: string) => void }) {
  if (tile.KIND === 'METRIC') {
    return <Metric tile={tile} onOpen={onOpen} />;
  }
  const common = defined({
    tileId: tile.NAME,
    title: tile.TITLE,
    subtitle: tile.SUBTITLE ?? undefined,
    footer: tile.FOOTER ?? undefined,
    icon: iconOf(tile.ICON),
    href: routeHash({ kind: 'app', app: tile.APP, params: {}, entity: null }),
    onPress: (e: { preventDefault(): void }) => {
      e.preventDefault();
      onOpen(tile.APP);
    },
  });
  if (tile.KIND === 'CONTENT') {
    const size = tile.SIZE === 'LARGE' ? '2x2' : tile.SIZE === 'WIDE' ? '2x1' : '1x1';
    return (
      <ContentTile {...common} size={size}>
        <p>{tile.CONTENT ?? ''}</p>
      </ContentTile>
    );
  }
  return <Tile {...common} size={tile.SIZE === 'WIDE' ? '2x1' : '1x1'} />;
}

/** The pages and sections of a space as groups of tiles. */
function groupsOf(space: HomeSpace, tiles: Map<string, HomeTile>, onOpen: (app: string) => void): TileGroupData[] {
  const groups: TileGroupData[] = [];
  for (const page of space.PAGES) {
    for (const section of page.SECTIONS) {
      const shown = section.TILES.map((n) => tiles.get(n)).filter((t): t is HomeTile => t !== undefined);
      if (shown.length === 0) {
        continue;
      }
      groups.push({
        id: `${page.NAME}/${section.ID}`,
        title: section.TITLE ?? page.TITLE,
        tiles: shown.map((t) => <TileOf key={t.NAME} tile={t} onOpen={onOpen} />),
      });
    }
  }
  return groups;
}

/** The start page: the spaces of the user as tabs. */
export function Home({ home, texts, space, onSpaceChange, onOpen }: HomeProps) {
  const tiles = new Map(home.TILES.map((t) => [t.NAME, t]));
  if (home.SPACES.length === 0) {
    return <p className="trfrontend-empty">{texts.noSpaces}</p>;
  }
  const selected = home.SPACES.some((s) => s.NAME === space) ? (space as string) : (home.SPACES[0]?.NAME ?? '');
  return (
    <Tabs
      aria-label={texts.spaces}
      items={home.SPACES.map((s) => ({ key: s.NAME, label: s.TITLE }))}
      selectedKey={selected}
      onSelectionChange={onSpaceChange}
      renderPanel={(key) => {
        const s = home.SPACES.find((x) => x.NAME === key);
        return s ? <TileGroups groups={groupsOf(s, tiles, onOpen)} /> : null;
      }}
    />
  );
}
