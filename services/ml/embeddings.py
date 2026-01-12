"""
Generate vector embeddings for semantic search using Sentence Transformers.
"""
import numpy as np
from sentence_transformers import SentenceTransformer
from typing import List, Dict
import torch
from dataclasses import dataclass
import logging


@dataclass
class ProjectEmbedding:
    project_id: str
    vector: np.ndarray
    metadata: Dict


class EmbeddingGenerator:
    """Generate embeddings for project descriptions and READMEs."""

    def __init__(self, model_name: str = "all-MiniLM-L6-v2", device: str = "cuda"):
        self.device = device if torch.cuda.is_available() else "cpu"
        self.model = SentenceTransformer(model_name, device=self.device)
        self.dimension = self.model.get_sentence_embedding_dimension()

        logging.info(f"Loaded embedding model: {model_name}")
        logging.info(f"Embedding dimension: {self.dimension}")
        logging.info(f"Using device: {self.device}")

    def generate(self, projects: List[Dict]) -> List[ProjectEmbedding]:
        """Generate embeddings for a batch of projects."""
        texts = self._prepare_texts(projects)

        # Generate embeddings in batches
        embeddings = self.model.encode(
            texts,
            batch_size=64,
            show_progress_bar=True,
            convert_to_numpy=True,
            normalize_embeddings=True  # L2 normalization for cosine similarity
        )

        result = []
        for i, project in enumerate(projects):
            result.append(ProjectEmbedding(
                project_id=project['id'],
                vector=embeddings[i],
                metadata={
                    'name': project['name'],
                    'language': project.get('language'),
                    'stars': project.get('stars', 0)
                }
            ))

        return result

    def generate_query_embedding(self, query: str) -> np.ndarray:
        """Generate embedding for a search query."""
        return self.model.encode(
            query,
            convert_to_numpy=True,
            normalize_embeddings=True
        )

    def _prepare_texts(self, projects: List[Dict]) -> List[str]:
        """Prepare text for embedding generation."""
        texts = []

        for project in projects:
            # Combine multiple fields for richer context
            parts = [
                project.get('name', ''),
                project.get('description', ''),
                project.get('readme_summary', ''),
                ', '.join(project.get('topics', [])),
                project.get('language', '')
            ]

            text = ' '.join(filter(None, parts))
            texts.append(text)

        return texts

    def compute_similarity(self, vec1: np.ndarray, vec2: np.ndarray) -> float:
        """Compute cosine similarity between two vectors."""
        return float(np.dot(vec1, vec2))  # Already normalized

    def find_similar(self, query_vec: np.ndarray, 
                     embeddings: List[np.ndarray], 
                     top_k: int = 10) -> List[tuple]:
        """Find top-k most similar embeddings."""
        similarities = []

        for i, emb in enumerate(embeddings):
            sim = self.compute_similarity(query_vec, emb)
            similarities.append((i, sim))

        # Sort by similarity (descending)
        similarities.sort(key=lambda x: x[1], reverse=True)

        return similarities[:top_k]


class HybridSearchEngine:
    """Combines semantic search with keyword matching."""

    def __init__(self, embedding_generator: EmbeddingGenerator, 
                 keyword_weight: float = 0.3):
        self.embedding_gen = embedding_generator
        self.keyword_weight = keyword_weight
        self.semantic_weight = 1.0 - keyword_weight

    def search(self, query: str, projects: List[Dict], top_k: int = 20) -> List[Dict]:
        """Hybrid search combining semantic and keyword matching."""
        # Semantic search
        query_vec = self.embedding_gen.generate_query_embedding(query)
        project_vecs = [p['embedding'] for p in projects]
        semantic_scores = self.embedding_gen.find_similar(query_vec, project_vecs, len(projects))

        # Keyword search
        keyword_scores = self._keyword_search(query, projects)

        # Combine scores
        combined_scores = {}
        for idx, sem_score in semantic_scores:
            combined_scores[idx] = sem_score * self.semantic_weight

        for idx, kw_score in enumerate(keyword_scores):
            if idx in combined_scores:
                combined_scores[idx] += kw_score * self.keyword_weight
            else:
                combined_scores[idx] = kw_score * self.keyword_weight

        # Sort and return top-k
        sorted_results = sorted(combined_scores.items(), key=lambda x: x[1], reverse=True)

        results = []
        for idx, score in sorted_results[:top_k]:
            result = projects[idx].copy()
            result['match_score'] = score
            results.append(result)

        return results

    def _keyword_search(self, query: str, projects: List[Dict]) -> List[float]:
        """Simple keyword matching using TF-IDF-like scoring."""
        query_terms = set(query.lower().split())
        scores = []

        for project in projects:
            text = f"{project.get('name', '')} {project.get('description', '')}".lower()

            # Count matching terms
            matches = sum(1 for term in query_terms if term in text)
            score = matches / len(query_terms) if query_terms else 0

            scores.append(score)

        return scores


# Example usage
if __name__ == "__main__":
    # Initialize embedding generator
    generator = EmbeddingGenerator()

    # Sample projects
    projects = [
        {
            'id': '1',
            'name': 'awesome-ml',
            'description': 'Machine learning tutorials for beginners',
            'topics': ['machine-learning', 'python', 'tutorials'],
            'language': 'Python'
        },
        {
            'id': '2',
            'name': 'web-starter',
            'description': 'Beginner-friendly web development projects',
            'topics': ['web-development', 'javascript', 'html'],
            'language': 'JavaScript'
        }
    ]

    # Generate embeddings
    embeddings = generator.generate(projects)

    print(f"Generated {len(embeddings)} embeddings")
    print(f"Embedding dimension: {embeddings[0].vector.shape}")

    # Search example
    query = "learn machine learning"
    query_vec = generator.generate_query_embedding(query)

    project_vecs = [e.vector for e in embeddings]
    similar = generator.find_similar(query_vec, project_vecs, top_k=2)

    print(f"\nTop matches for '{query}':")
    for idx, score in similar:
        print(f"  {embeddings[idx].metadata['name']}: {score:.3f}")
