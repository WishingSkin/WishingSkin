export interface TextureRecord { id: string; fileHash: string; name: string; description: string; kind: 'skin' | 'cape'; model: string; public: boolean; owned: boolean }
export interface LibraryRecord extends TextureRecord { ownerId: string; ownerName: string; displayName: string; collectionCount: number }
